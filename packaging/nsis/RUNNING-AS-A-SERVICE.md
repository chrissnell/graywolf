# Running Graywolf as a Windows Service

The Windows installer drops `graywolf.exe` and `graywolf-modem.exe` into
`C:\Program Files\Graywolf\` and adds a Start Menu shortcut. By default
Graywolf runs as a foreground console app under the user who launched it.

If you want it to run in the background and start automatically at boot,
register it as a Windows service yourself. The two common approaches:

## Option 1: NSSM (recommended)

[NSSM](https://nssm.cc/) wraps any console app as a proper Windows
service, including log rotation and crash recovery.

```powershell
# Install nssm via Chocolatey or download from https://nssm.cc/
choco install nssm

# Register graywolf as a service
nssm install Graywolf "C:\Program Files\Graywolf\graywolf.exe" `
  -config "C:\ProgramData\Graywolf\graywolf.db" `
  -history-db "C:\ProgramData\Graywolf\graywolf-history.db" `
  -tile-cache-dir "C:\ProgramData\Graywolf\tiles" `
  -modem "C:\Program Files\Graywolf\graywolf-modem.exe" `
  -http 127.0.0.1:8080

nssm set Graywolf Start SERVICE_AUTO_START
nssm set Graywolf AppStdout "C:\ProgramData\Graywolf\graywolf.log"
nssm set Graywolf AppStderr "C:\ProgramData\Graywolf\graywolf.log"
nssm start Graywolf
```

## Option 2: sc.exe (built in)

`sc.exe` ships with Windows but does not handle stdout/stderr or restart
policy on its own.

```powershell
sc.exe create Graywolf `
  binPath= "\"C:\Program Files\Graywolf\graywolf.exe\" -config \"C:\ProgramData\Graywolf\graywolf.db\" -history-db \"C:\ProgramData\Graywolf\graywolf-history.db\" -tile-cache-dir \"C:\ProgramData\Graywolf\tiles\" -modem \"C:\Program Files\Graywolf\graywolf-modem.exe\" -http 127.0.0.1:8080" `
  start= auto `
  DisplayName= "Graywolf APRS"

sc.exe start Graywolf
```

Note: `graywolf.exe` is a console application and does not implement the
Windows Service Control Protocol, so `sc.exe` will report a startup error
even when the process is running. NSSM avoids this because it implements
the SCP itself and supervises the child process.

## Removing the service

```powershell
# NSSM
nssm stop Graywolf
nssm remove Graywolf confirm

# sc.exe
sc.exe stop Graywolf
sc.exe delete Graywolf
```
