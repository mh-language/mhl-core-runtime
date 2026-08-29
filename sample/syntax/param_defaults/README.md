# param_defaults

A parameter may declare a default value — `greeting: string = "Hello"` or just
`b = 10` — used whenever the caller omits that argument. It works for `tool` method
parameters, lambda parameters, and `prompt` parameters (see
[../../features/prompts/prompt_default_parameter.mh](../../features/prompts/prompt_default_parameter.mh)).
Tool methods and lambdas bind positionally, so a defaulted parameter may not be followed
by a non-defaulted one (`mhl lint` flags it); `prompt` binds by name, so its defaults may
sit anywhere. The default expression is evaluated lazily, once per omitting call, in the
callee's own scope.

- [omitted_argument_uses_the_default.mh](omitted_argument_uses_the_default.mh) — a tool
  method and a lambda called with and without the optional argument
- [default_reads_an_earlier_parameter.mh](default_reads_an_earlier_parameter.mh) — because
  positional params bind left to right, `close: string = open` sees `open`'s value
- [too_many_arguments_is_rejected.mh](too_many_arguments_is_rejected.mh) — a default widens
  the accepted count to a range; exceeding the maximum still raises
