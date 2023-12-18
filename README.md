# awscost

The awscost is a tool to print AWS costs to text or graph image.

## Development

```
% make build
```

```
% AWS_PROFILE=${PROFILE_NAME} IS_LAMBA=false DRY_RUN=false ./dist/main
```

## Deployment

1. Secret

 Secret must have secret value`SLACK_BOT_TOKEN` and `SLACK_CHANNEL` as Key/Value.

2. IAM Role

Role must have policies `AWSLambdaBasicExecutionRole`, `CloudWatchLogsFullAccess` and following policy:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "ce:GetCostAndUsage",
                "ce:GetCostForecast",
                "organizations:ListAccounts"
            ],
            "Resource": "*"
        },
        {
            "Effect": "Allow",
            "Action": "secretsmanager:GetSecretValue",
            "Resource": "${SECRET_ARN}"
        }
    ]
}
```

3. Deploy

I recomment to use [fujiwara/lambroll](https://github.com/fujiwara/lambroll).

function.json:

```json
{
  "FunctionName": "awscost",
  "Handler": "bootstrap",
  "MemorySize": 128,
  "Role": "${IAM_ROLE_ARN}",
  "Runtime": "provided.al2",
  "Timeout": 20,
  "Environment": {
    "Variables": {
      "SECRET_NAME": "${SECRET_NAME}",
      "IS_LAMBDA": "true",
      "DRY_RUN": "false"
    }
  }
}
```

Download release and extract to `dist` directory:

```
curl -L https://github.com/takaishi/awscost/releases/download/v0.0.1/awscost_Linux_x86_64.tar.gz -o /tmp/awscost.tar.gz
tar -zxvf /tmp/awscost.tar.gz -C /tmp
mv /tmp/awscost ./dist/bootstrap
```

Deploy by lambroll:

```
lambroll deploy --function="function.json" --src="./dist"
```
