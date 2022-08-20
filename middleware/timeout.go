package middleware

// Heavily inspired by https://github.com/rookie-ninja/rk-echo/blob/master/middleware/timeout/middleware.go
// FIXME: Below 1ms difference between timeout and view handler finalization there is a response write datarace

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/neoxelox/kit"
)

type TimeoutConfig struct {
	Timeout time.Duration
}

type Timeout struct {
	kit.Middleware
	config   TimeoutConfig
	observer kit.Observer
}

func NewTimeout(observer kit.Observer, config TimeoutConfig) *Timeout {
	return &Timeout{
		config:   config,
		observer: observer,
	}
}

func (self *Timeout) Handle(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		timeoutCtx, cancel := context.WithTimeout(ctx.Request().Context(), self.config.Timeout)
		defer cancel()

		// Set deadline in request context
		ctx.SetRequest(ctx.Request().WithContext(timeoutCtx))

		finishChan := make(chan struct{}, 1)
		panicChan := make(chan interface{}, 1)
		timeoutChan := time.After(self.config.Timeout)

		timeoutHandlerCtx := _newTimeoutHandlerCtx(ctx)

		go func() {
			defer func() {
				if rec := recover(); rec != nil {
					panicChan <- rec

					// Handler panicked after timeout
					self.handlePanickedAfterTimeout(timeoutHandlerCtx, rec)
				}
			}()

			// Execute handler
			timeoutHandlerCtx.handlerError = next(timeoutHandlerCtx.handlerCtx)

			finishChan <- struct{}{}

			// Handler finished after timeout
			self.handleFinishedAfterTimeout(timeoutHandlerCtx)
		}()

		select {
		// Handler panicked
		case rec := <-panicChan:
			self.handlePanicked(timeoutHandlerCtx)
			// Repanic so upwards middlewares are aware of it
			panic(rec)
		// Handler finished on time
		case <-finishChan:
			self.handleFinished(timeoutHandlerCtx)
		// Handler timed out
		case <-timeoutChan:
			self.handleTimeout(timeoutHandlerCtx)
		}

		return timeoutHandlerCtx.handlerError
	}
}

type _timeoutHandlerCtx struct {
	bufferPool     *_bufferPool
	buffer         *bytes.Buffer
	originalWriter http.ResponseWriter
	timeoutWriter  *_timeoutResponseWriter
	handlerCtx     echo.Context
	handlerError   error
}

func _newTimeoutHandlerCtx(handlerCtx echo.Context) *_timeoutHandlerCtx {
	var timeoutHandlerCtx _timeoutHandlerCtx

	timeoutHandlerCtx.handlerCtx = handlerCtx
	timeoutHandlerCtx.bufferPool = _newBufferPool()
	timeoutHandlerCtx.buffer = timeoutHandlerCtx.bufferPool.Get()
	timeoutHandlerCtx.originalWriter = timeoutHandlerCtx.handlerCtx.Response().Writer
	// Wrap original writer in a timeout writer that will ignore operations after timeout
	timeoutHandlerCtx.timeoutWriter = _newTimeoutResponseWriter(
		timeoutHandlerCtx.originalWriter, timeoutHandlerCtx.buffer)
	timeoutHandlerCtx.handlerCtx.Response().Writer = timeoutHandlerCtx.timeoutWriter
	timeoutHandlerCtx.handlerError = nil

	return &timeoutHandlerCtx
}

func (self *Timeout) handlePanicked(ctx *_timeoutHandlerCtx) {
	ctx.timeoutWriter.mutex.Lock()
	defer ctx.timeoutWriter.mutex.Unlock()

	// Free timeout writer buffer
	ctx.timeoutWriter.FreeBuffer()
	ctx.bufferPool.Put(ctx.buffer)

	// Switch to original writer (Because nothing was written to timeout writer yet)
	ctx.handlerCtx.Response().Writer = ctx.originalWriter
}

func (self *Timeout) handleFinished(ctx *_timeoutHandlerCtx) {
	// Handle, serialize and write handler exception response to timeout writer
	if ctx.handlerError != nil {
		ctx.handlerCtx.Error(ctx.handlerError)
	}

	ctx.timeoutWriter.mutex.Lock()
	defer ctx.timeoutWriter.mutex.Unlock()

	// Copy handler response headers to original writer
	dst := ctx.timeoutWriter.ResponseWriter.Header()
	for k, v := range ctx.timeoutWriter.Header() {
		dst[k] = v
	}

	// Copy handler response status code to original writer
	ctx.timeoutWriter.ResponseWriter.WriteHeader(ctx.timeoutWriter.statusCode)

	// Copy handler response body to original writer
	_, err := ctx.timeoutWriter.ResponseWriter.Write(ctx.buffer.Bytes())
	if err != nil {
		panic(err)
	}

	// Free timeout writer buffer
	ctx.timeoutWriter.FreeBuffer()
	ctx.bufferPool.Put(ctx.buffer)
}

// TODO: could we just switch to original writer and return http.ErrHandlerTimeout upwards?
func (self *Timeout) handleTimeout(ctx *_timeoutHandlerCtx) {
	ctx.timeoutWriter.mutex.Lock()
	defer ctx.timeoutWriter.mutex.Unlock()

	// Set timeout writer timed out
	ctx.timeoutWriter.hasTimedOut = true

	// Free timeout writer buffer
	ctx.timeoutWriter.FreeBuffer()
	ctx.bufferPool.Put(ctx.buffer)

	// Switch to original writer (Because nothing was written to timeout writer yet)
	ctx.handlerCtx.Response().Writer = ctx.originalWriter

	// Handle, serialize and write timeout exception response to original writer
	ctx.handlerCtx.Error(http.ErrHandlerTimeout)

	// Switch back to timeout writer so that handler code executed after the timeout
	// cannot write to original writer anymore (it is ignored in the implementation)
	ctx.handlerCtx.Response().Writer = ctx.timeoutWriter
}

func (self *Timeout) handlePanickedAfterTimeout(ctx *_timeoutHandlerCtx, rec interface{}) {
	ctx.timeoutWriter.mutex.Lock()
	defer ctx.timeoutWriter.mutex.Unlock()

	if ctx.timeoutWriter.hasTimedOut {
		err, ok := rec.(error)
		if !ok {
			err = kit.ErrServerGeneric().With(fmt.Sprint(rec))
		}

		err = kit.ErrServerTimedOut().Withf("after executing %s %s", ctx.handlerCtx.Request().Method,
			ctx.handlerCtx.Request().RequestURI).Wrap(err)

		self.observer.Error(err)
	}
}

func (self *Timeout) handleFinishedAfterTimeout(ctx *_timeoutHandlerCtx) {
	ctx.timeoutWriter.mutex.Lock()
	defer ctx.timeoutWriter.mutex.Unlock()

	if ctx.timeoutWriter.hasTimedOut {
		err := kit.ErrServerTimedOut().Withf("after executing %s %s", ctx.handlerCtx.Request().Method,
			ctx.handlerCtx.Request().RequestURI).Wrap(ctx.handlerError)

		self.observer.Error(err)
	}
}

type _bufferPool struct {
	pool sync.Pool
}

func _newBufferPool() *_bufferPool {
	return &_bufferPool{}
}

func (self *_bufferPool) Get() *bytes.Buffer {
	buf := self.pool.Get()
	if buf == nil {
		return &bytes.Buffer{}
	}

	return buf.(*bytes.Buffer) // nolint
}

func (self *_bufferPool) Put(buf *bytes.Buffer) {
	self.pool.Put(buf)
}

type _timeoutResponseWriter struct {
	http.ResponseWriter
	body         *bytes.Buffer
	headers      http.Header
	mutex        sync.Mutex
	hasTimedOut  bool
	wroteHeaders bool
	statusCode   int
}

func _newTimeoutResponseWriter(w http.ResponseWriter, buf *bytes.Buffer) *_timeoutResponseWriter {
	return &_timeoutResponseWriter{ResponseWriter: w, body: buf, headers: make(http.Header)}
}

func (self *_timeoutResponseWriter) Header() http.Header {
	return self.headers
}

func (self *_timeoutResponseWriter) Write(body []byte) (int, error) {
	if self.hasTimedOut || self.body == nil {
		return 0, nil
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.body.Write(body) // nolint
}

func (self *_timeoutResponseWriter) WriteHeader(statusCode int) {
	if self.hasTimedOut || self.wroteHeaders {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.statusCode = statusCode
	self.wroteHeaders = true
}

func (self *_timeoutResponseWriter) FreeBuffer() {
	self.body = nil
}
