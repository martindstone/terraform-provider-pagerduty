module github.com/terraform-providers/terraform-provider-pagerduty

go 1.14

require (
	cloud.google.com/go v0.71.0 // indirect
	github.com/hashicorp/terraform-plugin-sdk v1.16.1
	github.com/martindstone/go-pagerduty/pagerduty v0.0.0-20210421033830-70c269a16857
	golang.org/x/tools v0.0.0-20201110124207-079ba7bd75cd // indirect
	google.golang.org/api v0.35.0 // indirect
	google.golang.org/genproto v0.0.0-20201109203340-2640f1f9cdfb // indirect
	google.golang.org/grpc v1.33.2 // indirect
)

// replace github.com/martindstone/go-pagerduty/pagerduty => ../go-pagerduty/pagerduty
