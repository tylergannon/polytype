module example.com/polytype-readiness

go 1.27

require (
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2
	github.com/tylergannon/polytype v1.0.0-rc.12
)

require golang.org/x/text v0.14.0 // indirect

replace github.com/tylergannon/polytype => ../..
