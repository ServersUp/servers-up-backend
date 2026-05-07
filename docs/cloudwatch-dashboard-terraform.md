# CloudWatch Dashboard (Terraform) — ServersUp Backend

This repo does not contain Terraform, but the snippet below can be copied into the **infra repo** to create a low-cost CloudWatch dashboard for ServersUp backend metrics.

This dashboard assumes **event metrics (Option B)** (adds/removes/status changes over a time window), not an exact “current total subscriptions” gauge.

## Assumptions

- **Namespace**: `ServersUp/Backend`
- **Dimensions**: `gameId` only (low-cardinality)
- **Metrics** (examples; must match what the Lambdas emit):
  - `ServersPolled` (Count), dimension `gameId`
  - `SubscriptionAdded` (Count), dimension `gameId`
  - `SubscriptionRemoved` (Count), dimension `gameId`
  - `StatusChange` (Count), dimension `gameId`

## Terraform example

```hcl
variable "aws_region" {
  type = string
}

variable "game_ids" {
  description = "Game IDs to render as separate lines (e.g. [\"wow\"])."
  type        = list(string)
  default     = ["wow"]
}

locals {
  namespace = "ServersUp/Backend"

  # Build CloudWatch metric arrays: [namespace, metricName, dimName, dimValue]
  status_change_metrics = [
    for g in var.game_ids : [local.namespace, "StatusChange", "gameId", g]
  ]

  subs_added_metrics = [
    for g in var.game_ids : [local.namespace, "SubscriptionAdded", "gameId", g]
  ]

  subs_removed_metrics = [
    for g in var.game_ids : [local.namespace, "SubscriptionRemoved", "gameId", g]
  ]

  servers_polled_metrics = [
    for g in var.game_ids : [local.namespace, "ServersPolled", "gameId", g]
  ]
}

resource "aws_cloudwatch_dashboard" "serversup_backend" {
  dashboard_name = "ServersUp-Backend"

  dashboard_body = jsonencode({
    start          = "-PT24H"
    periodOverride = "inherit"
    widgets = [
      {
        type   = "metric"
        x      = 0
        y      = 0
        width  = 12
        height = 6
        properties = {
          title   = "Status Changes (by game)"
          view    = "timeSeries"
          stacked = false
          region  = var.aws_region
          stat    = "Sum"
          period  = 300
          metrics = local.status_change_metrics
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 0
        width  = 12
        height = 6
        properties = {
          title  = "Subscriptions Added/Removed (by game)"
          view   = "timeSeries"
          region = var.aws_region
          stat   = "Sum"
          period = 300
          metrics = concat(
            local.subs_added_metrics,
            local.subs_removed_metrics
          )
        }
      },
      {
        type   = "metric"
        x      = 0
        y      = 6
        width  = 12
        height = 4
        properties = {
          title  = "Totals (windowed)"
          view   = "singleValue"
          region = var.aws_region
          stat   = "Sum"
          period = 86400
          metrics = [
            [local.namespace, "SubscriptionAdded"],
            [".", "SubscriptionRemoved"],
            [".", "StatusChange"]
          ]
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 6
        width  = 12
        height = 4
        properties = {
          title  = "Servers Polled (by game)"
          view   = "timeSeries"
          region = var.aws_region
          stat   = "Sum"
          period = 300
          metrics = local.servers_polled_metrics
        }
      }
    ]
  })
}
```

## Notes

- CloudWatch dashboards do not support wildcard dimensions; `game_ids` must be provided (or generated) in Terraform.
- The “Totals” widget is **windowed**: it shows the sum over the current dashboard time range (or the widget period), not a lifetime counter.

