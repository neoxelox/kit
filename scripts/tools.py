import superinvoke

from .tags import Tags


class Tools(superinvoke.Tools):
    Go = superinvoke.Tool(
        name="go",
        version=">=1.22.0",
        tags=[*Tags.As("*")],
        path="go",
    )

    Git = superinvoke.Tool(
        name="git",
        version=">=2.34.1",
        tags=[*Tags.As("*")],
        path="git",
    )

    Curl = superinvoke.Tool(
        name="curl",
        version=">=7.81.0",
        tags=[*Tags.As("*")],
        path="curl",
    )

    Test = superinvoke.Tool(
        name="gotestsum",
        version="1.11.0",
        tags=[Tags.DEV, Tags.CI_INT],
        links={
            superinvoke.Platforms.LINUX: (
                "https://github.com/gotestyourself/gotestsum/releases/download/v1.11.0/gotestsum_1.11.0_linux_amd64.tar.gz",
                "gotestsum",
            ),
            superinvoke.Platforms.MACOS: (
                "https://github.com/gotestyourself/gotestsum/releases/download/v1.11.0/gotestsum_1.11.0_darwin_arm64.tar.gz",
                "gotestsum",
            ),
            superinvoke.Platforms.WINDOWS: (
                "https://github.com/gotestyourself/gotestsum/releases/download/v1.11.0/gotestsum_1.11.0_windows_amd64.tar.gz",
                "gotestsum.exe",
            ),
        },
    )

    Lint = superinvoke.Tool(
        name="golangci-lint",
        version="1.55.2",
        tags=[Tags.DEV, Tags.CI_INT],
        links={
            superinvoke.Platforms.LINUX: (
                "https://github.com/golangci/golangci-lint/releases/download/v1.55.2/golangci-lint-1.55.2-linux-amd64.tar.gz",
                "golangci-lint-1.55.2-linux-amd64/golangci-lint",
            ),
            superinvoke.Platforms.MACOS: (
                "https://github.com/golangci/golangci-lint/releases/download/v1.55.2/golangci-lint-1.55.2-darwin-arm64.tar.gz",
                "golangci-lint-1.55.2-darwin-arm64/golangci-lint",
            ),
            superinvoke.Platforms.WINDOWS: (
                "https://github.com/golangci/golangci-lint/releases/download/v1.55.2/golangci-lint-1.55.2-windows-amd64.zip",
                "golangci-lint-1.55.2-windows-amd64/golangci-lint.exe",
            ),
        },
    )
