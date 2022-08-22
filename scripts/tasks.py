import os
import re

from superinvoke import console, rich, task

from .envs import Envs
from .tools import Tools


@task(
    help={
        "test": "[<PACKAGE_PATH>]::[<TEST_NAME>]. If empty, it will run all tests.",
        "verbose": "Show stdout of tests.",
        "show": "Show coverprofile page.",
    },
)
def test(context, test="", verbose=False, show=False):
    """Run tests."""

    test_arg = "./..."
    if test:
        test = test.split("::")
        if len(test) == 1 and test[0]:
            test_arg = f"{test[0]}/..."
        if len(test) == 2 and test[1]:
            test_arg += f" -run {test[1]}"

    verbose_arg = ""
    if verbose:
        verbose_arg = "-v"

    parallel_arg = ""
    if os.cpu_count():
        parallel_arg = f"--parallel={os.cpu_count()}"

    coverprofile_arg = ""
    if show:
        coverprofile_arg = "-coverprofile=coverage.out"

    result = context.run(
        f"{Tools.Test} --format=testname --no-color=False -- {verbose_arg} {parallel_arg} -race -count=1 -cover {coverprofile_arg} {test_arg}",
    )

    if "DONE 0 tests" not in result.stdout:
        packages = 0
        coverage = 0.0

        for cover in re.findall(r"[0-9]+\.[0-9]+(?=%)", result.stdout):
            packages += 1
            coverage += float(cover)

        if packages:
            coverage = round(coverage / packages, 1)

        console.print(
            rich.panel.Panel(
                f"Total Coverage ([bold]{packages} pkg[/bold]): [bold green]{coverage}%[/bold green]",
                expand=False,
            )
        )

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
    if Envs.Current != Envs.Ci:
        context.fail(f"publish command only available in {Envs.Ci} environment!")

    version = context.tag()
    if not version:
        latest_version = context.tag(current=False) or "v0.0.0"
        major, minor, patch = tuple(map(str, (latest_version.split("."))))
        version = f"{major}.{str(int(minor) + 1)}.{0}"
        context.info(f"Version tag not set, generating one from {latest_version}: {version}")
        context.run(f"{Tools.Git} tag {version}")
        context.run(f"{Tools.Git} push origin {version}")
    else:
        context.info(f"Version tag already set: {version}")

    context.info("Refreshing golang module registry cache")

    context.run(f"{Tools.Curl} 'https://sum.golang.org/lookup/github.com/neoxelox/kit@{version}'")
    context.run(f"{Tools.Curl} 'https://proxy.golang.org/github.com/neoxelox/kit/@v/{version}.info'")
