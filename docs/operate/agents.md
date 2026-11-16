# Agents

An **agent** is a small program on a monitored machine that reports its CPU, memory, disk and network to CertainStats. This page covers the supported agents, how to add, reinstall and remove one, and what the panel shows. It is for people running a server. Developers: see [agent internals](../develop/agents.md).

CertainStats does not ship its own agent. It speaks the protocols of three existing ones:

| Agent | How it reports | Notes |
| --- | --- | --- |
| **Beszel** (default) | Keeps a WebSocket open to the panel; the panel asks it for data | Needed for [network monitors](network-monitoring.md) (Beszel 0.20.0 or later). Authenticated with a token and a per-agent SSH key. |
| **LTstats** | Sends small binary reports to `POST /submit` | Does not work with a non-default panel path (`PANEL_PATH`). |
| **HetrixTools** | Sends compressed JSON reports to `POST /submit` | Install steps for regular Linux and Alpine. |

Agents are not the same as [collectors](collectors/README.md), which report what software such as CrowdSec or Knot DNS sees.

## Adding an agent

1. In the Agent Hub, choose to add an agent.
2. Pick the agent type (Beszel unless you choose otherwise) and, optionally, a name. Without a name, one is generated, such as `amber-falcon`. Names are at most 64 characters.
3. The install dialog shows what that agent needs:
   - **Beszel:** the Hub URL, the token, the hub public key, and a Docker Compose file or a Linux install script with those filled in.
   - **LTstats:** the token and a Debian install command. The command checks the installer's SHA-256 before running it.
   - **HetrixTools:** the token and an install script for Linux or Alpine Linux, rewritten to send to this panel's `/submit` instead of HetrixTools.
4. Run the install steps on the machine. The agent appears online when its first report arrives.

The URLs in the install steps use `PANEL_URL` when it is set, so they say `https` behind a reverse proxy; otherwise they are built from your request. The machine must be able to reach that URL. For Beszel, a reverse proxy must pass WebSocket upgrades and keep idle connections open longer than 30 seconds.

## Managing agents

The **Management** page (`/agents/management`) has one expandable row per agent, with search. Each row offers:

| Action | What it does |
| --- | --- |
| Show token | The token is masked; Show reveals it and Copy copies the full value. |
| Rename | Changes the agent's name. |
| Install | Shows the install steps again, with the current token and SSH key. |
| Uninstall | Shows the commands that remove the agent from the machine. |
| Reset token | Issues a new token and disconnects the agent. Reinstall it, or update its token, to reconnect. |
| Reset SSH key (Beszel) | Issues a new key pair and disconnects the agent. Update the agent's `KEY` to the new public key. |
| Delete | Removes the agent, its live connection, its cached readings and all of its stored metrics. Archived network monitor history is kept. |

An agent's type is fixed when it is created: a report from a different kind of agent using its token is refused.

Notes on an agent (up to 10,000 characters) and its name are edited in place on its detail view.

## The Agent Hub

- **Headline and fleet figures.** A sentence such as "1 node is offline. The other 5 are healthy.", then bandwidth and disk I/O now, and traffic and disk totals.
- **Cards or table.** Each agent shows CPU, memory and disk use with meters, a 24-hour CPU sparkline, live network rates and uptime. Offline agents are marked. Search by name, CPU or ID. The view choice is remembered.
- **Detail view.** Clicking an agent opens its detail at `/{agent_id}` without leaving the page: hardware tiles, storage per mount, history charts, notes and network monitors.

Values update live every ten seconds.

## How data behaves

- **Report interval.** The panel asks Beszel agents for data every `BESZEL_EVERY` seconds (default: `UPDATE_EVERY`, 60). Full system details (CPU model, OS, sizes) are requested on connect and then every 30th time. LTstats and HetrixTools report on their own schedule.
- **Offline.** An agent is marked offline after 3 minutes without a report.
- **Gaps** stay empty: missing readings are never stored as zeros.
- **Network totals.** Beszel reports totals since the machine booted. The first report after the panel starts only sets a baseline, and a counter that goes backwards (a reboot) is treated as missing, not as a huge negative.
- **Lifetime totals** of network traffic and disk I/O count what was observed since the agent was created. Deleting and re-adding an agent starts them again.
- **Resent reports.** LTstats and HetrixTools reports carry their own timestamps, so a report sent twice is stored once. See [troubleshooting](troubleshooting.md#agents-look-offline-or-charts-have-gaps).

## Limits

- An agent HTTP report is at most 16 KiB; a Beszel WebSocket message at most 1 MiB.
- Rate limits and connection limits are in [security](security.md#limits).
