# Network monitoring

A **network monitor** checks a target, such as a host, port, URL or DNS name, from one of your agents, so the check runs from that machine's network. This page covers what monitors can do, how to manage them, and how their charts and alerts behave. It is for people running a server. Developers: see [network monitoring internals](../develop/network-monitoring.md).

Monitors are private: only their owner sees them, and their data never appears on public dashboards.

## Requirements

| Agent | Network monitors |
| --- | --- |
| Beszel 0.20.0 or later | ICMP, TCP, HTTP/HTTPS and DNS checks |
| Beszel 0.21.0 or later | Also custom DNS servers and certificate details |
| LTstats, HetrixTools | Not supported |

When you create or edit a monitor, every selected agent is checked. An agent that cannot run that kind of check is refused, with the agent, the feature and the reason.

## Managing monitors

Monitors are managed on the **Network** page, or from a node's details.

- **Interval:** how often the agent checks, from 60 to 3600 seconds. The default is 60.
- **Several agents at once:** creating the same monitor on several agents creates one independent monitor per agent.
- **Limits:** up to 100 current monitors per agent. A duplicate of an existing monitor is refused.
- **Agents that are offline** pick up changes when they reconnect.

What an edit does to history:

| Change | Effect |
| --- | --- |
| Interval | History is kept. |
| Target, protocol, TCP port or DNS server | The old monitor is archived with its history, and a new one is created. Alert rules move to the new one. |
| Pause or archive | Open incidents close as "monitoring removed". History is kept. |
| Delete the node | Its monitors are archived. |

## The Network page

### Graphs

The page opens with **response time** and **failed checks** graphs above the monitor list.

- **Filters** apply to both the graphs and the list:
  - hostname or target text, matched case-insensitively, including hostnames inside URLs;
  - protocol: `icmp`, `tcp`, `http` or `dns`;
  - node;
  - state.
- The graphs show the first ten matching monitors, in the list's order, independently of list paging.
- A legend grouped by target and protocol toggles each node's line in both graphs. Colours stay stable.
- Changing a filter resets the line toggles, but keeps the time window and zoom. Refreshing keeps all three.
- **Time controls** are the same as on the agent graphs: presets, a custom start/end picker, and the remembered `certainstats_active_hours` choice (six hours if none). Brushing (dragging across) either chart updates both charts and the picker. The picker's **Reset** clears the custom range and returns to the last preset.
- If one line fails to load, it shows a retry, and the others stay visible.
- On a node's page, graphs load when you open the section.

**Reading the graphs:**
- Response times are in milliseconds. A gap means no reading, or that every check in that period failed.
- Beszel can report overlapping windows, so the graphs summarise the windows reported, not an exact count of unique checks.

### Monitor list and groups

The list groups monitors by target (for HTTP checks, the full URL). Each group shows its nodes, and each node's settings, readings and controls.

- Groups follow the order of their first matching monitor. Paging still counts monitors, so one target can appear on several pages.
- **Group dropdown:** selects one exact target across pages and filters both the list and the graphs. **All groups** clears it.
  - The node, protocol and state filters narrow the dropdown's options; target search and the selected group do not.
  - A selected group stays available even when nothing matches it.
- Groups start expanded and collapse by their heading. Collapsed groups stay collapsed through refresh, filtering, paging and switching views, until a full page reload.
- Counts in group headings describe matching monitors on the current page.
- On a node's page the list is flat, with no Group dropdown.

### Editing a group

**Edit group** loads every current monitor for the target, across nodes and pages, whatever the list filters are.

- A group shares its target, protocol, port, DNS resolver and interval. New monitors added to an existing target take its settings.
- Monitors can still be paused or removed one by one. An individual edit that would give a group mixed settings, or a duplicate of a target on one agent, is refused.
- The group editor keeps each monitor's paused or running state by default, starts added monitors running, and offers **Pause all** and **Resume all**.
- Removing a monitor from the group archives it. At least one must remain.
- Renaming a group to a target that already has a group is refused.
- If someone else changed the group meanwhile, saving is refused; reload and try again.
- An older group whose monitors have mixed settings is refused with a clear message, rather than guessing which settings to keep.

### Live updates

- Monitor readings update live every ten seconds, over the panel's one live connection.
- If the connection drops, or network readings stop arriving for 45 seconds, the cards fall back to polling.
- Graphs refresh every ten seconds while visible.
- Switching between the agent overview, agent detail and Network keeps filters, paging, ranges and zoom. Hidden views pause their refreshes. Direct links, reloads and Back/Forward work as usual.

## Alerts

Use the `network_loss` condition on the Alerts page. How rules, targets and incidents work in general is in [alerts](alerts.md).

- A rule selects **monitors**, not agents.
- It fires when loss is above (`>`) a threshold from 0 to 100 percent, over a fixed one-hour window.
- It is only judged when the agent is online and able to run the check, the monitor has at least three readings, and the latest reading is fresh. Fresh means within three check intervals, but at least three minutes and at most one hour.
- Stale, offline, unsupported or unknown monitors neither fire nor recover.
- Each rule and monitor pair is tracked separately.
- Incidents record the target, protocol and DNS server, with the rule and node.
- Firing and recovery use the normal notification delivery and retries.
- Removing a monitor closes its incidents without sending a false "recovered" notification.

## Removing a node

Revoking a node clears its cached readings from memory. Archived network history stays in the TSDB.
