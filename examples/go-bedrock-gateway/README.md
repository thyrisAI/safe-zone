# Safe Zone + AWS Bedrock Example

This example demonstrates how to use Safe Zone with AWS Bedrock as the AI provider.

## Prerequisites

1. **AWS Credentials**: Configure AWS credentials using one of the standard methods:
   - Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
   - Shared credentials file (`~/.aws/credentials`)
   - IAM role (when running on EC2, ECS, Lambda, etc.)

2. **Bedrock Access**: Ensure you have access to AWS Bedrock in your region and have enabled the models you want to use.

3. **Safe Zone Server**: The Safe Zone server must be running with Bedrock configuration.

## Required IAM Permissions

Your AWS credentials need the following IAM permissions:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "bedrock:InvokeModel",
                "bedrock:InvokeModelWithResponseStream"
            ],
            "Resource": [
                "arn:aws:bedrock:*::foundation-model/anthropic.claude-*",
                "arn:aws:bedrock:*::foundation-model/amazon.titan-*",
                "arn:aws:bedrock:*::foundation-model/meta.llama*",
                "arn:aws:bedrock:*::foundation-model/mistral.*",
                "arn:aws:bedrock:*::foundation-model/cohere.*",
                "arn:aws:bedrock:*::foundation-model/openai.*"
            ]
        }
    ]
}
```

## Configuration

### Environment Variables

Set the following environment variables to configure Safe Zone for Bedrock:

```bash
# Required: Set provider to Bedrock
export AI_PROVIDER=BEDROCK

# Required: AWS region where Bedrock is available
export AWS_BEDROCK_REGION=us-east-1

# Optional: Model ID (defaults to Claude 3 Sonnet)
export AWS_BEDROCK_MODEL_ID=anthropic.claude-3-sonnet-20240229-v1:0

# Optional: Custom endpoint for VPC endpoints
# export AWS_BEDROCK_ENDPOINT_OVERRIDE=https://vpce-xxx.bedrock-runtime.us-east-1.vpce.amazonaws.com
```

### Supported Models

Safe Zone supports the following Bedrock model families:

| Model Family | Example Model ID | Notes |
|--------------|------------------|-------|
| Anthropic Claude | `anthropic.claude-3-sonnet-20240229-v1:0` | Recommended for most use cases |
| Amazon Titan | `amazon.titan-text-express-v1` | Good for general text generation |
| Meta Llama | `meta.llama3-8b-instruct-v1:0` | Open-source alternative |
| Mistral | `mistral.mistral-7b-instruct-v0:2` | Fast inference |
| Cohere | `cohere.command-text-v14` | Good for summarization |
| OpenAI | `openai.gpt-4o-mini-2024-07-18-v1:0` | GPT models via Bedrock |

## Running the Example

### 1. Start Safe Zone with Bedrock

```bash
# From the project root
AI_PROVIDER=BEDROCK \
AWS_BEDROCK_REGION=us-east-1 \
AWS_BEDROCK_MODEL_ID=anthropic.claude-3-sonnet-20240229-v1:0 \
go run main.go
```

### 2. Run the Example Client

```bash
# From this directory
go run main.go
```

Or specify a custom TSZ URL:

```bash
TSZ_URL=http://localhost:8080 go run main.go
```

## Example Output

```
=== Safe Zone + AWS Bedrock Example ===
TSZ Gateway URL: http://localhost:8080

--- Example 1: Simple Chat Completion ---
Response: The capital of France is Paris.

--- Example 2: Chat with PII Detection ---
Response: Of course! I'd be happy to help you. What do you need assistance with?
TSZ Metadata: map[guardrails:[] input:[...] output:[...] rid:LLM-GW-...]

--- Example 3: Chat with Guardrails ---
Response: Why don't scientists trust atoms? Because they make up everything!
```

## Troubleshooting

### "Failed to reach upstream LLM service"

- Check that AWS credentials are properly configured
- Verify the Bedrock region is correct
- Ensure the model is enabled in your AWS account

### "Access Denied" errors

- Verify IAM permissions include `bedrock:InvokeModel`
- Check that the model ARN in the policy matches the model you're using

### "Model not found" errors

- Ensure the model is available in your region
- Verify the model ID is correct (check AWS Bedrock console)
- Some models require explicit enablement in the AWS console

## Security Considerations

1. **Credentials**: Never commit AWS credentials to version control
2. **VPC Endpoints**: For production, consider using VPC endpoints for Bedrock
3. **KMS**: Bedrock supports KMS encryption for data at rest
4. **Logging**: Enable CloudTrail for audit logging of Bedrock API calls
