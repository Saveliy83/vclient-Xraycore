# vclient

**vclient** is an independent fork of [Xray-core](https://github.com/XTLS/Xray-core), an open platform for network proxying.

This repository is a personal project and may include custom changes. Unless stated otherwise, features and behavior are inherited from the upstream Xray-core project.

## About Xray-core

Xray-core is a platform for various network proxying use cases. For detailed information about supported protocols, configuration, and usage, see the [upstream Xray-core documentation](https://github.com/XTLS/Xray-core).

## Build

This project is written in Go. To build the `xray` executable from the repository root:

### Linux / macOS

```bash
CGO\_ENABLED=0 go build -o xray -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -v ./main

Windows (PowerShell)
powershell

\$env:CGO\_ENABLED=0
go build -o xray.exe -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -v ./main

Upstream

    Source project: XTLS/Xray-core
    Examples: XTLS/Xray-examples
    Documentation and usage information: see the upstream project

License

Xray-core is distributed under the Mozilla Public License 2.0 (MPL-2.0). See the LICENSE [blocked] file for the license applicable to this repository and its modifications.
Disclaimer

This repository is an independent fork and is not an official XTLS/Xray-core project. Use it in accordance with applicable laws and the licenses of the project and its dependencies.

