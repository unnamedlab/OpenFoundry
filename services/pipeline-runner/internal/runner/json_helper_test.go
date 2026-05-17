package runner

import "encoding/json"

// jsonMarshalImpl is split into its own file so args_test.go can
// reference a function whose import block doesn't pollute the main
// test surface with encoding/json. Keeps lint warnings about unused
// imports off the table while letting the helper stay private.
func jsonMarshalImpl(v any) ([]byte, error) { return json.Marshal(v) }
