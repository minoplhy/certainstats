# Alerts

An **alert rule** watches agents, network monitors or collector subjects, and sends a notification when a condition is met and again when it clears. This page covers creating rules, notification targets, message templates and incident history. It is for people running a server.

Everything lives on the **Alerts** page (`/alerts`): rules, saved targets and incident history.

## Creating a rule

**New alert rule** opens a dialog:

| Field | Meaning |
| --- | --- |
| **Name** | Your label for the rule. |
| **Enabled** | Disabled rules are not evaluated. |
| **When** | The condition; see below. |
| **Operator** and **Threshold** | Above (`>`), below (`<`) or equal to (`=`) a number, in the condition's unit. Not used for "server goes offline". |
| **For at least** | How long the condition is averaged over before it fires, as a Go duration such as `5m` or `1h`. |
| **Send to** | A saved target, a Discord webhook URL, or a custom webhook URL. |
| **Message template** | Optional JSON for a custom webhook; see [templates](#message-templates). |
| **Servers** / **Monitors** / **Watch** | What the rule applies to: servers for resource conditions, monitors for network loss, collector subjects for collector conditions. |

A rule must watch the right kind of thing for its condition: servers for resource rules, monitors for network rules, and collector subjects for collector rules. Servers you add later are not included automatically; edit the rule to add them.

### Conditions

| When | Unit | How it is measured |
| --- | --- | --- |
| Server goes offline (`agent_down`) | none | The agent has sent nothing for 3 minutes |
| CPU usage, CPU I/O wait, CPU steal | % | Average over "For at least" |
| Memory usage, swap usage, disk usage | % of the size | Average used bytes over "For at least", divided by the RAM, swap or total disk size. Skipped while the size is unknown. |
| Download rate, upload rate, disk read rate, disk write rate | KiB/s | Average rate over "For at least", weighted by sample duration; disks are added together |
| Network monitor loss (`network_loss`) | % | Loss over the past hour; see [network monitoring](network-monitoring.md#alerts) |
| Knot DNS conditions (`knot_*`) | varies | See [Knot DNS](collectors/knot.md#alert-rules) |
| CrowdSec conditions (`crowdsec_*`) | varies | See [CrowdSec](collectors/crowdsec.md#alert-rules) |

Network and collector conditions set their own timing: they have a threshold only, no operator or duration.

### How rules are evaluated

- Rules are evaluated every `UPDATE_EVERY` seconds (60 by default).
- Each server (or monitor, or subject) on a rule has its own state: **OK** or **firing**.
- When the condition becomes true, the rule **fires** for that server and sends a notification.
- When it becomes false again, the rule **resolves** and sends a recovery notification.
- Missing or stale data never resolves an incident on its own: an agent that stops reporting does not count as healthy.

## Notification targets

A **target** is a saved destination that several rules can share. Create them in the targets section of the Alerts page:

| Field | Meaning |
| --- | --- |
| **Name** | Your label for the target. |
| **Send to** | Discord webhook, or HTTP POST webhook. |
| **Webhook URL** | Where to send. |
| **Default message template** | Optional JSON, used when a rule doesn't give its own. |

Both rules and targets can send a **test notification**, filled with sample values ("Test Node"), to check the destination before relying on it.

Webhook rules:
- Only HTTP and HTTPS URLs are accepted, without embedded credentials.
- Requests time out after 10 seconds and follow at most three redirects, never to another host.
- Internal network addresses are allowed.

### What is sent by default

- **Discord:** an embed, red "Alert Triggered" or green "Alert Resolved", with the agent, the condition, the current value, and for offline alerts the down time and duration.
- **HTTP POST:** a JSON object with `agent_id`, `nickname`, `trigger_type`, `status`, `value`, `operator`, `threshold` and `time`, plus monitor and collector fields when they apply.

## Message templates

A template is the JSON body to send instead of the default. Put placeholders inside JSON strings; they are filled in and escaped, so names with quotes keep the JSON valid. **Template variables** in the dialog lists them.

| Placeholder | Value |
| --- | --- |
| `{{NICKNAME}}` | Server name (for collector rules: what the rule watches) |
| `{{AGENT_ID}}` | Server ID |
| `{{TRIGGER_TYPE}}` | Condition ID, such as `cpu_usage` |
| `{{TRIGGER_LABEL}}` | Condition name |
| `{{STATUS}}` | `FIRING` or `RESOLVED` |
| `{{VALUE}}` | Formatted current value, such as `92.50%` or `512.00 KiB/s` |
| `{{OPERATOR}}`, `{{THRESHOLD}}` | The rule's condition |
| `{{TIME_TRIGGER}}`, `{{TIME_RESOLVED}}` | When the incident started and ended (RFC 3339; Discord timestamps in Discord payloads) |
| `{{DOWN_DURATION}}` | How long a server was offline |
| `{{MONITOR_ID}}`, `{{MONITOR_TARGET}}`, `{{MONITOR_PROTOCOL}}`, `{{DNS_SERVER}}` | Network monitor rules |
| `{{COLLECTOR}}`, `{{SUBJECT_KIND}}`, `{{DETAIL}}` | Collector rules: collector name, what kind of thing is watched, what is wrong |

Example:

```json
{"text": "{{STATUS}}: {{TRIGGER_LABEL}} on {{NICKNAME}} is {{VALUE}} ({{OPERATOR}} {{THRESHOLD}})"}
```

## Incident history

Every firing is an **incident**, listed in the incident history with its rule, subject, start and end times, value, and the delivery result of each notification.

- A banner at the top of the Alerts page shows how many incidents are active.
- Each incident lists its events: firing, each notification attempt, recovery.
- **Failed notifications are retried automatically** on each evaluation, for up to 24 hours. You can also retry one by hand.
- An incident closes **without** a recovery notification when its rule is disabled or deleted, or no longer covers that server, monitor or subject, or when monitoring is removed (for example, a network monitor is paused or archived).

## Limits

- Only the owner sees their rules, targets and incidents.
- Rules and targets are changed through the panel, or through the [API](../develop/architecture.md#alerts) with a [CSRF token](security.md#scripts-and-api-clients-csrf).
