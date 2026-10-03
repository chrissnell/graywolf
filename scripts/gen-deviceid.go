// Command gen-deviceid regenerates pkg/aprs/deviceid_data.go from the
// upstream aprsorg/aprs-deviceid tocalls.yaml file.
//
// Usage:
//
//	curl -o /tmp/tocalls.yaml https://raw.githubusercontent.com/aprsorg/aprs-deviceid/main/tocalls.yaml
//	go run scripts/gen-deviceid.go /tmp/tocalls.yaml > pkg/aprs/deviceid_data.go
//	gofmt -w pkg/aprs/deviceid_data.go
package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type deviceEntry struct {
	Vendor string `yaml:"vendor"`
	Model  string `yaml:"model"`
	Class  string `yaml:"class"`
}

type tocallsFile struct {
	Tocalls []struct {
		Tocall      string `yaml:"tocall"`
		deviceEntry `yaml:",inline"`
	} `yaml:"tocalls"`
	Mice []struct {
		Suffix      string `yaml:"suffix"`
		deviceEntry `yaml:",inline"`
	} `yaml:"mice"`
	MiceLegacy []struct {
		Prefix      string `yaml:"prefix"`
		Suffix      string `yaml:"suffix"`
		deviceEntry `yaml:",inline"`
	} `yaml:"micelegacy"`
}

func goStr(s string) string {
	return fmt.Sprintf("%q", s)
}

func infoLit(e deviceEntry) string {
	return fmt.Sprintf("DeviceInfo{%s, %s, %s}", goStr(e.Vendor), goStr(e.Model), goStr(e.Class))
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gen-deviceid <tocalls.yaml>")
		os.Exit(1)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}

	var f tocallsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}

	var b strings.Builder
	b.WriteString("package aprs\n\n")
	b.WriteString("// Auto-generated from https://github.com/aprsorg/aprs-deviceid tocalls.yaml\n")
	b.WriteString("// Licensed under CC BY-SA 2.0 by Hessu, OH7LZB\n")
	b.WriteString("// Regenerate with: go run scripts/gen-deviceid.go tocalls.yaml > pkg/aprs/deviceid_data.go\n\n")

	b.WriteString("var tocallDB = []tocallEntry{\n")
	for _, e := range f.Tocalls {
		fmt.Fprintf(&b, "\t{%s, %s},\n", goStr(e.Tocall), infoLit(e.deviceEntry))
	}
	b.WriteString("}\n\n")

	b.WriteString("var miceDB = map[string]DeviceInfo{\n")
	for _, e := range f.Mice {
		fmt.Fprintf(&b, "\t%s: {%s, %s, %s},\n", goStr(e.Suffix), goStr(e.Vendor), goStr(e.Model), goStr(e.Class))
	}
	b.WriteString("}\n\n")

	b.WriteString("var miceLegacyDB = []miceLegacyEntry{\n")
	for _, e := range f.MiceLegacy {
		fmt.Fprintf(&b, "\t{%s, %s, %s},\n", goStr(e.Prefix), goStr(e.Suffix), infoLit(e.deviceEntry))
	}
	b.WriteString("}\n")

	fmt.Print(b.String())
}
