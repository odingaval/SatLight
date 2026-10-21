# Localization Plan

Target regions named in the proposal: Kenya, Argentina, Japan, Brazil.

| Locale | Languages | Notes |
|---|---|---|
| Launch (v1) | English | Plain language; short sentences for non-native readers |
| Phase 2 | Spanish (es), Portuguese (pt-BR), Japanese (ja), Swahili (sw) | Prioritize by waitlist data |

## Engineering
- Externalize all strings (no copy in templates or code); use ICU message format.
- Locale-aware number formatting; show sats with thousands separators and approximate fiat in the user's currency (KES, ARS, JPY, BRL).
- Dates and times in the user's time zone; store UTC.
- Right-to-left readiness for future locales.

## Content
- Glossary per language for "sats", "Lightning", "escrow", "bounty". Do not translate "sats" and "Lightning" unless a community term exists.
- Community review of translations; translations are themselves bounty tasks.

## Wallet guidance
Maintain a per-region list of Lightning wallets that support Lightning addresses and are available in that country; verify before publishing.
