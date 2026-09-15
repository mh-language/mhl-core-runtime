# tool_scope

A `tool` body can declare `const name = expression` and `var name = expression`
alongside its methods. Initializers run in declaration order for each method call,
so later declarations can read earlier ones. Every method sees these bindings;
a method can assign to a tool `var`, while a tool `const` remains read-only.
Each call starts with a fresh tool environment, so changes to a `var` do not
persist into the next call.

- [const_is_available_to_every_method.mh](const_is_available_to_every_method.mh) —
  two methods read the same tool constants
- [var_initializes_in_order_and_resets_per_call.mh](var_initializes_in_order_and_resets_per_call.mh) —
  a tool variable uses an earlier constant, changes during a call, and resets
  for the next call
- [reassigning_a_tool_const_is_rejected.mh](reassigning_a_tool_const_is_rejected.mh) —
  assigning to a tool constant raises a catchable error

Run all three with `mhl test sample/syntax/tool_scope`.
