# BAX Weekly Usage Metric Contract

This reference captures the known reusable metric contract from prior BAX usage
investigations. Verify live fields before each run; do not treat this file as a
replacement for dataset metadata.

## Primary Source

- `bytedcli --site i18n-tt --json aeolus dataset-fields -r sg 3574811`
- Dataset `3574811`: `【公会BAX】Agent X Session X message 统计数据集`
- Region: `sg`
- Business line filter: `tenant_id_type = 'bax-am'`
- Business date: derive from `min_span_created_time` when available.
- Latest partition check: use the dataset partition/date fields available in
  the current schema, then cross-check the max business date.

## Core Metrics

- WAU: distinct effective user id over the week.
- DAU: distinct effective user id by business date; report daily series and
  daily average.
- Sessions: distinct `session_id`.
- Messages: distinct `log_id` or the closest available message id field.
- Missing identity: rows whose user id cannot be mapped to a stable
  `feishu_open_id` or equivalent user key; report separately.

When field names drift, read `dataset-fields`, then query a tiny sample before
writing the final SQL. Fail fast on missing fields that materially change the
metric instead of silently switching to an incompatible approximation.

## Weekly Windows

Use half-open date windows:

```text
target:   [target_monday, target_monday + 7 days)
previous: [target_monday - 7 days, target_monday)
```

The scheduled run should use the most recent completed natural week in
Asia/Shanghai unless the caller specifies another timezone.

## Non-RD Filter

For the "不含产研" view, previous BAX chart work inferred the filter from the
online report as excluding department names containing:

- `国际直播`
- `TikTok-Design-LIVE`
- `TikTok LIVE-Platform`
- `研发`
- `设计`

Before using the filter, check that the dataset still has a department field
with comparable values. If department data is missing or has drifted, report
that the non-RD view is unavailable instead of guessing from usernames.

## Known Historical Anchors

- Old dashboard `740208`, sheet `1023684`, dataset `3459500` was useful for
  BAX Weekly up to W34 in the 2026-09-08 investigation.
- That old source used `role=user` and weekly distinct `feishu_open_id` for
  WAU, distinct `session_id` for sessions, and distinct `log_id` for messages.
- It lagged the W35 business dates during the prior investigation, so it is now
  a comparison source only.

## Interpretation Rules

- Data freshness comes before business interpretation.
- WAU down and messages up usually means user coverage and depth moved in
  opposite directions; inspect cohorts before concluding health recovered.
- A single strong region, department, agent, or skill can explain a large share
  of net change, but only call it the cause when the aggregate impact is clear.
- Product or GTM explanations require supporting evidence from messages,
  releases, campaigns, or known operational cadence. Otherwise mark them as
  hypotheses.
