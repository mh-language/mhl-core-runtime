# object_property_access

Object literals, dotted property access, dynamic `obj[key]` access, and the errors each
raises when a key is missing or of the wrong type.

- [object_property_access.mh](object_property_access.mh) — reading fields with `.key`
- [object_property_access_with_exception.mh](object_property_access_with_exception.mh) —
  reading a missing field raises a catchable error
- [object_property_access_with_nested_objects.mh](object_property_access_with_nested_objects.mh)
  — chaining `.key` through nested objects
- [object_property_access_with_arrays.mh](object_property_access_with_arrays.mh) — a field
  holding an array, indexed after the dot
- [object_property_access_with_complex_structure.mh](object_property_access_with_complex_structure.mh)
  — nested object containing an array
- [object_property_access_with_non_existent_nested_key.mh](object_property_access_with_non_existent_nested_key.mh)
  — a missing nested key raises a catchable error
- [object_property_access_with_non_existent_array_index.mh](object_property_access_with_non_existent_array_index.mh)
  — an out-of-range index on a field's array raises a catchable error
- [object_get_keys_and_values.mh](object_get_keys_and_values.mh) — `.keys()` and `.values()`
- [object_dynamic_key_access.mh](object_dynamic_key_access.mh) — `obj[key]` with a key held
  in a variable
- [object_dynamic_key_access_with_computed_key.mh](object_dynamic_key_access_with_computed_key.mh)
  — the key expression is evaluated at runtime, not resolved at parse time
- [object_dynamic_key_access_with_exception.mh](object_dynamic_key_access_with_exception.mh)
  — `obj[key]` on a missing key raises a catchable error
- [object_dynamic_key_access_with_non_string_key.mh](object_dynamic_key_access_with_non_string_key.mh)
  — object keys must be strings; a numeric key raises a catchable error
- [object_dynamic_key_access_with_nested_objects.mh](object_dynamic_key_access_with_nested_objects.mh)
  — chaining `obj[k1][k2]` through nested objects
- [object_dynamic_key_access_mixed_with_array.mh](object_dynamic_key_access_mixed_with_array.mh)
  — `obj[key][i]`, dynamic key then array index
- [object_dynamic_key_write.mh](object_dynamic_key_write.mh) — `obj[key] = value` adds a new
  field
- [object_dynamic_key_write_overwrites_existing_key.mh](object_dynamic_key_write_overwrites_existing_key.mh)
  — `obj[key] = value` overwrites an existing field
- [object_dynamic_key_write_with_nested_objects.mh](object_dynamic_key_write_with_nested_objects.mh)
  — writing through a dynamic key into a nested object
