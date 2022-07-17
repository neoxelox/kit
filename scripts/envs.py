import superinvoke

from .tags import Tags


class Envs(superinvoke.Envs):
    Default = lambda cls: cls.Dev

    Dev = superinvoke.Env(
        name="dev",
        tags=[Tags.DEV],
    )

    Ci = superinvoke.Env(
        name="ci",
        tags=[Tags.CI],
    )

    Prod = superinvoke.Env(
        name="prod",
        tags=[Tags.PROD],
    )
