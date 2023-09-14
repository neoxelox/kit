import superinvoke

from .tags import Tags


class Tools(superinvoke.Tools):
    Go = superinvoke.Tool(
        name="go",
        version=">=1.21.0",
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
        version=">=1.10.1",
        tags=[Tags.DEV, Tags.CI_INT],
        links={
            superinvoke.Platforms.LINUX: (
                "https://github.com/gotestyourself/gotestsum/releases/download/v1.10.1/gotestsum_1.10.1_linux_amd64.tar.gz",
                "gotestsum",
            ),
            superinvoke.Platforms.MACOS: (
                "https://github.com/gotestyourself/gotestsum/releases/download/v1.10.1/gotestsum_1.10.1_darwin_arm64.tar.gz",
                "gotestsum",
            ),
        },
    )

    Lint = superinvoke.Tool(
        name="golangci-lint",
        version=">=1.54.2",
        tags=[Tags.DEV, Tags.CI_INT],
        links={
            superinvoke.Platforms.LINUX: (
                "https://github.com/golangci/golangci-lint/releases/download/v1.54.2/golangci-lint-1.54.2-linux-amd64.tar.gz",
                "golangci-lint-1.54.2-linux-amd64/golangci-lint",
            ),
            superinvoke.Platforms.MACOS: (
                "https://github.com/golangci/golangci-lint/releases/download/v1.54.2/golangci-lint-1.54.2-darwin-arm64.tar.gz",
                "golangci-lint-1.54.2-darwin-arm64/golangci-lint",
            ),
        },
    )
