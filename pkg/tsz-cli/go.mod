module github.com/thyrisAI/safe-zone/pkg/tsz-cli

go 1.25.0

replace github.com/thyrisAI/safe-zone/pkg/tszclient-go => ../tszclient-go

require (
	github.com/spf13/cobra v1.10.2
	github.com/thyrisAI/safe-zone/pkg/tszclient-go v0.0.0-00010101000000-000000000000
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
)
