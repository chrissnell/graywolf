package redact

import "github.com/chrissnell/graywolf/pkg/flareschema"

// ScrubFlare applies eng.Apply to every user-visible string field
// across every section of f, including nested log entry attrs. Slices
// are walked element-by-element; maps are walked entry-by-entry.
//
// The function is hand-rolled rather than reflection-driven so:
//   - the schema's privileged fields (callsigns, schema_version,
//     enum-like Direction / Kind values) can be deliberately skipped
//   - changes to the schema are surfaced as compile errors here rather
//     than silently bypassed by reflection
//
// Adding a new section to flareschema.Flare requires adding a scrub
// branch here; the lack of generality is intentional.
func ScrubFlare(f *flareschema.Flare, eng *Engine) {
	if f == nil || eng == nil {
		return
	}

	// User
	f.User.Email = eng.Apply(f.User.Email)
	f.User.Notes = eng.Apply(f.User.Notes)
	f.User.RadioModel = eng.Apply(f.User.RadioModel)
	f.User.AudioInterface = eng.Apply(f.User.AudioInterface)

	// Meta — versions, commits, hashes are deliberately NOT scrubbed.
	// HostnameHash is already a hash; submitted_at is a fixed format.

	// Config — values only; keys are configstore-internal and
	// already key-scrubbed at source by configdump.
	for i := range f.Config.Items {
		f.Config.Items[i].Value = eng.Apply(f.Config.Items[i].Value)
	}
	scrubIssues(f.Config.Issues, eng)

	// System
	f.System.OSPretty = eng.Apply(f.System.OSPretty)
	f.System.Kernel = eng.Apply(f.System.Kernel)
	f.System.PiModel = eng.Apply(f.System.PiModel)
	for i := range f.System.UdevRulesPresent {
		f.System.UdevRulesPresent[i] = eng.Apply(f.System.UdevRulesPresent[i])
	}
	for i := range f.System.NetworkInterfaces {
		f.System.NetworkInterfaces[i].Name = eng.Apply(f.System.NetworkInterfaces[i].Name)
		// MACOUI is already OUI-only by collector design — leave alone.
	}
	scrubIssues(f.System.Issues, eng)

	// ServiceStatus
	f.ServiceStatus.Manager = eng.Apply(f.ServiceStatus.Manager)
	f.ServiceStatus.Unit = eng.Apply(f.ServiceStatus.Unit)
	scrubIssues(f.ServiceStatus.Issues, eng)

	// PTT candidates — Path/Description carry hostnames + paths.
	for i := range f.PTT.Candidates {
		f.PTT.Candidates[i].Path = eng.Apply(f.PTT.Candidates[i].Path)
		f.PTT.Candidates[i].Description = eng.Apply(f.PTT.Candidates[i].Description)
		f.PTT.Candidates[i].Owner = eng.Apply(f.PTT.Candidates[i].Owner)
	}
	scrubIssues(f.PTT.Issues, eng)

	// GPS candidates
	for i := range f.GPS.Candidates {
		f.GPS.Candidates[i].Path = eng.Apply(f.GPS.Candidates[i].Path)
		f.GPS.Candidates[i].Description = eng.Apply(f.GPS.Candidates[i].Description)
		f.GPS.Candidates[i].Owner = eng.Apply(f.GPS.Candidates[i].Owner)
	}
	scrubIssues(f.GPS.Issues, eng)

	// AudioDevices — host/device names sometimes contain the hostname
	// (cpal exposes user-set device aliases on macOS).
	for i := range f.AudioDevices.Hosts {
		f.AudioDevices.Hosts[i].Name = eng.Apply(f.AudioDevices.Hosts[i].Name)
		for j := range f.AudioDevices.Hosts[i].Devices {
			f.AudioDevices.Hosts[i].Devices[j].Name = eng.Apply(f.AudioDevices.Hosts[i].Devices[j].Name)
		}
	}
	scrubIssues(f.AudioDevices.Issues, eng)

	// USBTopology — vendor/product strings can carry user-configured
	// names (printer queues etc.). Vendor/product IDs are 4-hex; safe.
	for i := range f.USBTopology.Devices {
		f.USBTopology.Devices[i].VendorName = eng.Apply(f.USBTopology.Devices[i].VendorName)
		f.USBTopology.Devices[i].ProductName = eng.Apply(f.USBTopology.Devices[i].ProductName)
		f.USBTopology.Devices[i].Manufacturer = eng.Apply(f.USBTopology.Devices[i].Manufacturer)
		f.USBTopology.Devices[i].Serial = eng.Apply(f.USBTopology.Devices[i].Serial)
	}
	scrubIssues(f.USBTopology.Issues, eng)

	// CM108
	for i := range f.CM108.Devices {
		f.CM108.Devices[i].Path = eng.Apply(f.CM108.Devices[i].Path)
		f.CM108.Devices[i].Description = eng.Apply(f.CM108.Devices[i].Description)
	}
	scrubIssues(f.CM108.Issues, eng)

	// Logs
	f.Logs.Source = eng.Apply(f.Logs.Source)
	for i := range f.Logs.Entries {
		f.Logs.Entries[i].Msg = eng.Apply(f.Logs.Entries[i].Msg)
		f.Logs.Entries[i].Component = eng.Apply(f.Logs.Entries[i].Component)
		for k, v := range f.Logs.Entries[i].Attrs {
			if s, ok := v.(string); ok {
				f.Logs.Entries[i].Attrs[k] = eng.Apply(s)
			}
		}
	}
	scrubIssues(f.Logs.Issues, eng)
}

func scrubIssues(issues []flareschema.CollectorIssue, eng *Engine) {
	for i := range issues {
		issues[i].Message = eng.Apply(issues[i].Message)
		issues[i].Path = eng.Apply(issues[i].Path)
	}
}
