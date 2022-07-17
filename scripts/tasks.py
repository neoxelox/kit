import os
import re

from superinvoke import task

from .envs import Envs
from .tools import Tools


@task(
    help={
        "test": "<PACKAGE_PATH>::<TEST_NAME>. If empty, it will run all tests.",
        "verbose": "Show stdout of tests.",
        "show": "Show coverprofile page.",
    },
)
def test(context, test="", verbose=False, show=False):
    """Run tests."""

    test = test.split("::")

    test_arg = "./..."
    if len(test) == 2:
        test_arg = f"-run {test[1]} {test[0]}"

    verbose_arg = ""
    if verbose:
        verbose_arg = "-v"

    parallel_arg = ""
    if os.cpu_count():
        parallel_arg = f"--parallel={os.cpu_count()}"

    coverprofile_arg = ""
    if show:
        coverprofile_arg = "-coverprofile=coverage.out"

    r = context.run(
        f"{Tools.Test} --format=testname --no-color=False -- {verbose_arg} {parallel_arg} -race -count=1 -cover {coverprofile_arg} {test_arg}",
    )

    packages = 0
    coverage = 0.0

    for cover in re.findall(r"[0-9]+\.[0-9]+(?=%)", r.stdout):
        packages += 1
        coverage += float(cover)

    if packages:
        coverage = round(coverage / packages, 1)

    title = "=" * (len(str(packages) + str(coverage)) + 34)
    context.info(title, f"    Total Coverage ({packages} pkg) : {coverage}%", title, sep="\n")

    if show:
        context.run(f"{Tools.Go} tool cover -html=coverage.out")
        context.remove("coverage.out")


@task()
def lint(context):
    """Run linter."""

    context.run(f"{Tools.Lint} run ./... -c .golangci.yaml")


@task()
def format(context):
    """Run formatter."""

    context.run(f"{Tools.Lint} run ./... -c .golangci.yaml --fix")


@task()
def publish(context):
    """Publish package."""
    if Envs.Current != Envs.Prod:
        context.fail(f"publish command only available in {Envs.Prod} environment!")

    version = context.tag()
    if not version:
        latest_version = context.tag(current=False) or "v0.0.0"
        major, minor, patch = tuple(map(str, (latest_version.split("."))))
        version = f"{major}.{str(int(minor) + 1)}.{patch}"
        context.info(f"Version tag not set, generating one from {latest_version}: {version}")
        context.run(f"{Tools.Git} tag {version}")
        context.run(f"{Tools.Git} push --follow-tags")
    else:
        context.info(f"Version tag already set: {version}")

    context.info("Refreshing golang module registry cache")

    context.run(f"{Tools.Curl} 'https://sum.golang.org/lookup/github.com/neoxelox/kit@{version}'")
    context.run(f"{Tools.Curl} 'https://proxy.golang.org/github.com/neoxelox/kit/@v/{version}.info'")
    context.run(
        f"{Tools.Go} get github.com/neoxelox/kit@{version}",
        env={"GOPROXY": "https://proxy.golang.org", "GO111MODULE": "on"},
    )
