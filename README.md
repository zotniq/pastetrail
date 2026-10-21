# PasteTrail

A Go library and CLI for observing paste attempts and their destination context.
Events describe attempts, with explicit evidence and capability limits.

```sh
go build -o bin/pastetrail ./cmd/pastetrail
bin/pastetrail doctor
bin/pastetrail daemon --log pastetrail.jsonl
go test -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

`daemon` runs until interrupted and can be supervised by an OS service manager.
It emits JSON Lines: startup/capability changes and paste-attempt records.
Text and rich text are limited to 1 MiB per event by default. File references
contain metadata only; file contents are never opened. Logs contain clipboard
text and should be stored accordingly.

```go
monitor, err := pastetrail.New(pastetrail.Options{})
if err != nil { return err }
return monitor.Run(ctx, func(record pastetrail.Record) error {
    return json.NewEncoder(output).Encode(record)
})
```

Native platform support is under development; `doctor` reports the current build's capabilities.

Licensed under Apache-2.0. See [LICENSE](LICENSE).
