module github.com/lxc/lxcri

require (
	github.com/cpuguy83/go-md2man/v2 v2.0.0 // indirect
	github.com/creack/pty v1.1.11
	github.com/drachenfels-de/gocapability v0.0.0-20210413092208-755d79b01352
	github.com/kr/pretty v0.2.1 // indirect
	github.com/lxc/go-lxc v0.0.0-20210525154540-76e43f70a7f1
	github.com/opencontainers/runtime-spec v1.0.3-0.20200929063507-e6143ca7d51d
	github.com/rs/zerolog v1.20.0
	github.com/stretchr/testify v1.6.1
	github.com/urfave/cli/v2 v2.3.0
	golang.org/x/sys v0.0.0-20210521203332-0cec03c779c1
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	sigs.k8s.io/yaml v1.2.0
)

replace golang.org/x/crypto => golang.org/x/crypto v0.0.0-20201221181555-eec23a3978ad

replace golang.org/x/text => golang.org/x/text v0.3.3

go 1.16
