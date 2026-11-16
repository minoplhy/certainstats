# CertainStats

CertainStats is a self-hosted server monitoring platform in a single Go binary.

- **Agents** report system metrics: CPU, memory, disk and network. Beszel, LTstats and HetrixTools agents are supported.
- **Collectors** report what software on a host sees, such as [CrowdSec](docs/operate/collectors/crowdsec.md) alerts and [Knot DNS](docs/operate/collectors/knot.md) traffic.
- **Network monitors** check hosts, ports, URLs and DNS from your agents.
- A private **admin panel** shows everything live, with alert rules and notifications; **public status pages** show the parts you choose.

## Quick start

```sh
cp docker/docker-compose.yaml .
docker compose up -d
docker compose logs certainstats   # open the first-time setup link printed here
```

Then read [install and upgrade](docs/operate/install-and-upgrade.md) for backups and upgrades, and [configuration](docs/operate/configuration.md) to put the panel behind your own domain.

## Documentation

Everything is in [docs/](docs/README.md), split into guides for running a server and references for changing the code.

## License

[MIT](LICENSE)
