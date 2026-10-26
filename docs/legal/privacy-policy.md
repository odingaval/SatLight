# Satlight Privacy Policy (DRAFT)

> **Internal draft for privacy counsel. Do not publish until the operating entity, contact details, jurisdictions, processors, and retention schedule are confirmed.**

Last updated: 2026-10-26

**Release status:** This draft describes anticipated processing. The public preview should not be used to submit real payment or sensitive personal data.

Last updated: [date]

## What we collect
| Data | Source | Purpose |
|---|---|---|
| GitHub ID, login, avatar, email | GitHub sign-in | Account, notifications, fraud prevention |
| Lightning address, BOLT11 invoices, payment hashes and payment status | You / Lightning node | Display payment instructions, account for receipts, prevent duplicate credits, support reconciliation and investigations |
| Repository and issue/PR metadata | GitHub App | Run bounties, verify merges |
| Payment records (amounts, timestamps, payment hashes) | Lightning node, ledger | Operate escrow, accounting, support, legal |
| Waitlist email | You | Updates about launch |
| Device, IP address, logs | Automatic | Security, abuse prevention, debugging |
| Support messages | You | Resolve requests |

The current application does not provide a government-ID collection flow. Whether identity documents, sanctions screening, or other verification are required must be decided before money movement is enabled; do not interpret the absence of a form as a promise that no such information will be required. [COUNSEL DECISION.]

## How we use it
Provide and secure the service, process payments, prevent fraud and abuse, communicate with you, comply with law, and improve the product using aggregated metrics.

## What is public
The current app does not publish user profiles or payout totals as a public activity feed. Repository, issue, and pull-request metadata may already be public on GitHub. Lightning payment details are not written to the Bitcoin blockchain as readable invoice metadata, but transaction amounts and payment information may be visible to the node operator and relevant counterparties. [COUNSEL: verify this description for selected payment architecture.]

## Sharing
We share data with: GitHub (authentication and API), infrastructure and email providers acting as processors, professional advisers, and authorities when legally required. We do not sell personal data.

## Retention
Keep account data only while needed to provide the service or meet legal obligations. Retain financial and security records only for the period required by applicable law and documented operational need. The exact schedule for account deletion, financial records, invoices/payment hashes, support messages, and security logs is unresolved and must be set with counsel before launch; do not publish this draft as-is.

## Your rights
Depending on applicable law, you may have rights to be informed, access, correction, deletion, portability, or objection. Requests should be directed to the verified privacy contact for the operating entity. Some records may need to be retained for legal claims, accounting, or security. The rights, response periods, identity checks, and lawful bases must be reviewed against each launch jurisdiction. [COUNSEL DECISION.]

## Security
The intended controls include encryption in transit, access controls, least-privilege credentials, audit logging, backups, and incident response. Storage encryption and operational controls must be confirmed against the actual deployment before this statement is published.

## International transfers
Hosting regions, subprocessors, and transfer safeguards must be listed after production infrastructure is selected. [COUNSEL / OPERATIONS DECISION.]

## Children
Satlight is not for people under 18.

## Cookies and analytics
Essential cookies for sessions. Privacy-friendly analytics described in the analytics plan; opt-out available.

## Changes and contact
We will post updates here. Contact: [verified privacy contact for the operating entity].
