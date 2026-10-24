package argodiff

import "testing"

func TestDecodeRequiresStrictDocumentShape(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		input   string
		invalid bool
	}{
		{name: "empty resources", input: `{"resources":[]}`},
		{
			name: "resource with optional fields",
			input: `{"resources":[{"group":"apps","kind":"Deployment","namespace":"default",` +
				`"name":"api","diff":"- old\n+ new","versionMetadataOnly":true}]}`,
		},
		{name: "missing resources", input: `{}`, invalid: true},
		{name: "null resources", input: `{"resources":null}`, invalid: true},
		{name: "array document", input: `[]`, invalid: true},
		{name: "unknown document field", input: `{"resources":[],"unexpected":true}`, invalid: true},
		{
			name:    "unknown resource field",
			input:   `{"resources":[{"kind":"Service","name":"api","diff":"change","unexpected":true}]}`,
			invalid: true,
		},
		{name: "trailing object", input: `{"resources":[]} {}`, invalid: true},
		{name: "trailing text", input: `{"resources":[]} trailing`, invalid: true},
		{name: "missing kind", input: `{"resources":[{"name":"api","diff":"change"}]}`, invalid: true},
		{name: "blank name", input: `{"resources":[{"kind":"Service","name":" ","diff":"change"}]}`, invalid: true},
		{name: "blank diff", input: `{"resources":[{"kind":"Service","name":"api","diff":"\n"}]}`, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document, err := Decode([]byte(test.input))
			if (err != nil) != test.invalid {
				t.Fatalf("Decode() = %#v, %v", document, err)
			}
			if !test.invalid && document.Resources == nil {
				t.Fatal("resources array was lost")
			}
		})
	}
}
