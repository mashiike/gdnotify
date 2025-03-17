
resource "aws_iam_role" "main" {
  name = "gdnotify"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Sid    = ""
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      }
    ]
  })
}

resource "aws_iam_role_policy" "main" {
  name = "gdnotify"
  role = aws_iam_role.main.name
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = [
          "ssm:GetParameter*",
          "ssm:DescribeParameters",
          "ssm:List*",
        ]
        Effect   = "Allow"
        Resource = "*"
      },
      {
        Action = [
          "dynamodb:PutItem",
          "dynamodb:GetItem",
          "dynamodb:UpdateItem",
          "dynamodb:CreateTable",
          "dynamodb:DescribeTable",
          "dynamodb:DescribeTimeToLive",
          "dynamodb:UpdateTimeToLive",
          "dynamodb:Scan",
          "dynamodb:DeleteItem",
        ],
        Effect   = "Allow"
        Resource = "*",
      },
      {
        Action = [
          "lambda:GetFunctionUrlConfig",
        ],
        Effect   = "Allow"
        Resource = "*",
      },
      {
        Action = [
          "events:PutEvents",
        ],
        Effect   = "Allow"
        Resource = "*",
      },
    ]
  })
}

resource "aws_iam_role_policy_attachment" "execution_role" {
  role       = aws_iam_role.main.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "archive_file" "dummy" {
  type        = "zip"
  output_path = "${path.module}/dummy.zip"
  source {
    content  = "dummy"
    filename = "bootstrap"
  }
  depends_on = [
    null_resource.dummy
  ]
}

resource "null_resource" "dummy" {}

resource "aws_lambda_function" "main" {
  lifecycle {
    ignore_changes = all
  }

  function_name = "gdnotify"
  role          = aws_iam_role.main.arn
  architectures = ["arm64"]
  handler       = "bootstrap"
  runtime       = "provided.al2023"
  filename      = data.archive_file.dummy.output_path
}

resource "aws_lambda_alias" "main" {
  lifecycle {
    ignore_changes = all
  }
  name             = "current"
  function_name    = aws_lambda_function.main.arn
  function_version = aws_lambda_function.main.version
}

resource "aws_ssm_parameter" "google_application_credentials" {
  lifecycle {
    ignore_changes = [value]
  }
  name        = "/gdnotify/GOOGLE_APPLICATION_CREDENTIALS"
  description = "GOOGLE_APPLICATION_CREDENTIALS for gdnotify"
  type        = "SecureString"
  value       = "dummy"
}

resource "aws_cloudwatch_event_bus" "main" {
  name = "gdnotify"
}

// for EventBridge Scheduler

resource "aws_iam_role" "scheduler" {
  name = "gdnotify-scheduler"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Sid    = ""
        Principal = {
          Service = "scheduler.amazonaws.com"
        }
      }
    ]
  })
}

resource "aws_iam_role_policy" "scheduler" {
  name = "gdnotify-scheduler"
  role = aws_iam_role.scheduler.name
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = [
          "lambda:InvokeFunction",
        ]
        Effect = "Allow"
        Resource = [
          aws_lambda_alias.main.arn,
          aws_lambda_function.main.arn,
        ],
      }
    ]
  })
}


resource "aws_scheduler_schedule" "scheduler" {
  name = "gdnotify"

  flexible_time_window {
    mode = "OFF"
  }

  schedule_expression = "rate(15 minutes)"

  target {
    arn      = aws_lambda_alias.main.arn
    role_arn = aws_iam_role.scheduler.arn
  }
}
