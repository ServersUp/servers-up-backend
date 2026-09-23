# Feedback & Reporting — Operational Runbook

This runbook covers the implemented `/feedback` and `/report` flows and the
feedback email pipeline. These flows are **implemented but not yet live**;
they become live only after the deployment order below is completed and the
commands are registered.

## Email contract (Terraform)

- **Fixed sender:** `feedback@armasn.dev`
- **Fixed recipient:** `feedback+armasndev@proton.me`
- These values are the current Terraform contract.
- **SES identity verification / DKIM** must be completed for the sender
  domain before email can be sent.
- **Terraform apply is CI-only.** Do not apply Terraform manually outside the
  CI pipeline.

## Environment variables

- **Feedback Lambda:** `SES_FROM_EMAIL` and `SES_TO_EMAIL`.
- **Discord bot:** the same env contract (`SES_FROM_EMAIL` and `SES_TO_EMAIL`).

## Website contract

- **Canonical origin:** `https://serversup.armasn.dev`
- **Payload:** plain JSON with a `message` field and an optional
  `subscriptionId` field.
- **`FEEDBACK_API_URL`** on the website is **intentionally empty** until
  Terraform output supplies the real Function URL.

## Discord contract

- **`/report`** requires a bounded `subscription_id`.
- **No user reply-email field** is collected for either command.

## Logging

- Logs are **safe only**: no message body, no raw request, and no provider
  content is logged.

## Deployment order

1. Terraform
2. Backend
3. Command registration
4. Site endpoint configuration / publish

## Rollback

- Disable the endpoint and/or unregister the commands.

## Not in this release

The following are **explicitly not included** in this release:

- Rate limiting
- Honeypot / timing traps
- Abuse protection
- CAPTCHA
- WAF
- Duplicate suppression
- Idempotency

## Notes

- This document contains **no secrets** and **no message examples**.
- The flows described here are **not live** until the deployment order above
  is completed.
