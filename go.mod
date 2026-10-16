module github.com/nicksenap/grove

go 1.26.0

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/spf13/cobra v1.10.2
	golang.org/x/sys v0.49.0
	golang.org/x/term v0.46.0
)

require (
	github.com/fzipp/gocyclo v0.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/telemetry v0.0.0-20261009151841-97e8ea77aa2a // indirect
	golang.org/x/tools v0.52.0 // indirect
	honnef.co/go/tools v0.8.2 // indirect
)

tool (
	github.com/fzipp/gocyclo/cmd/gocyclo
	golang.org/x/tools/cmd/deadcode
	honnef.co/go/tools/cmd/staticcheck
)
