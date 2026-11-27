/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kea

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// SubstituteCredentials returns a copy of a rendered Kea configuration in
// which every string value exactly equal to one of the placeholders is
// replaced by its credential. Placeholders are the "$NAME" strings the
// renderers put in credential fields (database user and password, Control
// Agent password, TSIG secret), so only whole credential fields change:
// substrings are never touched and a credential that looks like another
// placeholder stays literal, because each value is visited once.
//
// The configuration is decoded and re-encoded with encoding/json, so every
// character of a credential is escaped correctly, and numbers keep their
// exact text. Errors never include configuration or credential text.
func SubstituteCredentials(configJSON []byte, values map[string]string) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(configJSON))
	dec.UseNumber()
	var doc interface{}
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decoding rendered configuration: invalid JSON")
	}
	doc = substitute(doc, values)
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding configuration with credentials failed")
	}
	return out, nil
}

func substitute(v interface{}, values map[string]string) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, child := range t {
			t[k] = substitute(child, values)
		}
		return t
	case []interface{}:
		for i, child := range t {
			t[i] = substitute(child, values)
		}
		return t
	case string:
		if value, ok := values[t]; ok {
			return value
		}
		return t
	default:
		return v
	}
}
