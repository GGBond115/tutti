module github.com/tutti-os/tutti/packages/connector/runtime

go 1.24.3

toolchain go1.24.5

require (
	github.com/tutti-os/tutti/packages/connector/host v0.0.0
	golang.org/x/mod v0.33.0
	golang.org/x/sys v0.41.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

replace github.com/tutti-os/tutti/packages/connector/host => ../host

replace google.golang.org/genproto => google.golang.org/genproto v0.0.0-20260120221211-b8f7ae30c516
