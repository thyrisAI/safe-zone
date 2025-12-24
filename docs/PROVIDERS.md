# TSZ AI Provider Guide

This document provides comprehensive information about AI providers supported by TSZ (Thyris Safe Zone), including configuration, usage, and best practices.

---

## Table of Contents

1. [Overview](#overview)
2. [Provider Architecture](#provider-architecture)
3. [Supported Providers](#supported-providers)
4. [Configuration](#configuration)
5. [Provider Comparison](#provider-comparison)
6. [Migration Guide](#migration-guide)
7. [Troubleshooting](#troubleshooting)
8. [Best Practices](#best-practices)

---

## Overview

TSZ supports multiple AI providers through a unified abstraction layer. This allows you to:

- **Switch providers** without changing application code
- **Use different providers** for different environments (dev, staging, production)
- **Leverage provider-specific features** while maintaining compatibility
- **Optimize for cost, latency, or compliance** requirements

### Supported Provider Types

1. **OpenAI-Compatible** - Works with any OpenAI-compatible API
2. **AWS Bedrock** - Native integration with AWS Bedrock service

---

## Provider Architecture

### Abstraction Layer

TSZ uses a `ChatProvider` interface defined in `internal/ai/provider.go`:

```go
type ChatProvider interface {
    Name() string
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, <-chan error)
    SupportsStreaming() bool
}
```

### Key Components

1. **Provider Factory** - Initializes the appropriate provider based on configuration
2. **Request/Response Translation** - Converts between OpenAI format and provider-specific formats
3. **Error Handling** - Unified error handling across providers
4. **Credential Management** - Provider-specific authentication mechanisms

### Data Flow

```
Client Request (OpenAI format)
    ↓
TSZ Gateway
    ↓
Provider Abstraction Layer
    ↓
Provider-Specific Implementation
    ↓
Upstream AI Service (OpenAI, Bedrock, etc.)
    ↓
Provider-Specific Response
    ↓
OpenAI-Compatible Response
    ↓
Client
```

---

## Supported Providers

### 1. OpenAI-Compatible Provider

**Provider ID:** `OPENAI_COMPATIBLE`

#### Description

The OpenAI-Compatible provider works with any service that implements the OpenAI Chat Completions API. This includes:

- **OpenAI** - Official OpenAI API
- **Azure OpenAI** - Microsoft's Azure OpenAI Service
- **Ollama** - Local LLM runtime
- **LM Studio** - Local LLM development tool
- **vLLM** - High-throughput LLM serving
- **Text Generation Inference** - Hugging Face's inference server
- **Any custom OpenAI-compatible endpoint**

#### Configuration

```env
AI_PROVIDER=OPENAI_COMPATIBLE
AI_MODEL_URL=https://api.openai.com/v1
AI_API_KEY=sk-your-api-key-here
AI_MODEL=gpt-4
```

#### Configuration Parameters

| Parameter | Required | Description | Example |
|-----------|----------|-------------|---------|
| `AI_MODEL_URL` | Yes | Base URL of the API endpoint | `https://api.openai.com/v1` |
| `AI_API_KEY` | Yes* | API key for authentication | `sk-...` |
| `AI_MODEL` | Yes | Default model name | `gpt-4` |

*Not required for services like Ollama that don't use authentication

#### Examples

**OpenAI:**
```env
AI_PROVIDER=OPENAI_COMPATIBLE
AI_MODEL_URL=https://api.openai.com/v1
AI_API_KEY=sk-proj-...
AI_MODEL=gpt-4
```

**Azure OpenAI:**
```env
AI_PROVIDER=OPENAI_COMPATIBLE
AI_MODEL_URL=https://your-resource.openai.azure.com/openai/deployments/your-deployment
AI_API_KEY=your-azure-key
AI_MODEL=gpt-4
```

**Ollama (Local):**
```env
AI_PROVIDER=OPENAI_COMPATIBLE
AI_MODEL_URL=http://localhost:11434/v1
AI_API_KEY=ollama
AI_MODEL=llama3.1:8b
```

**Ollama (Docker):**
```env
AI_PROVIDER=OPENAI_COMPATIBLE
AI_MODEL_URL=http://host.docker.internal:11434/v1
AI_API_KEY=ollama
AI_MODEL=llama3.1:8b
```

#### Features

- ✅ Non-streaming requests
- ✅ Streaming requests (SSE)
- ✅ Custom headers
- ✅ Timeout configuration
- ✅ Automatic retries (via HTTP client)

#### Limitations

- Authentication is limited to Bearer token
- No built-in support for AWS SigV4 signing
- Requires network connectivity to the endpoint

---

### 2. AWS Bedrock Provider

**Provider ID:** `BEDROCK`

#### Description

The AWS Bedrock provider offers native integration with Amazon Bedrock, AWS's fully managed service for foundation models. This provider uses the AWS SDK for Go v2 and supports all Bedrock model families.

#### Configuration

```env
AI_PROVIDER=BEDROCK
AWS_BEDROCK_REGION=us-east-1
AWS_BEDROCK_MODEL_ID=anthropic.claude-3-sonnet-20240229-v1:0
AWS_BEDROCK_ENDPOINT_OVERRIDE=  # Optional
```

#### Configuration Parameters

| Parameter | Required | Description | Example |
|-----------|----------|-------------|---------|
| `AWS_BEDROCK_REGION` | Yes | AWS region where Bedrock is available | `us-east-1` |
| `AWS_BEDROCK_MODEL_ID` | Yes | Bedrock model identifier | `anthropic.claude-3-sonnet-20240229-v1:0` |
| `AWS_BEDROCK_ENDPOINT_OVERRIDE` | No | Custom endpoint URL (for VPC endpoints) | `https://vpce-xxx.bedrock-runtime.us-east-1.vpce.amazonaws.com` |

#### AWS Credentials

Bedrock uses the standard AWS credential chain:

1. **Environment Variables:**
   ```env
   AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
   AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
   AWS_SESSION_TOKEN=...  # Optional, for temporary credentials
   ```

2. **Shared Credentials File:**
   ```
   ~/.aws/credentials
   ```

3. **IAM Role:**
   - Automatically used when running on EC2, ECS, Lambda, etc.

#### Required IAM Permissions

TSZ needs IAM permissions to invoke Bedrock models. You can configure this in several ways:

##### Option 1: IAM User with Access Keys (Development/Testing)

1. **Create IAM Policy** in AWS Console:
   - Go to IAM → Policies → Create Policy
   - Select JSON tab and paste:

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

2. **Name the policy**: `TSZ-Bedrock-Access`

3. **Create IAM User**:
   - Go to IAM → Users → Create User
   - Name: `tsz-bedrock-user`
   - Attach the policy: `TSZ-Bedrock-Access`

4. **Create Access Keys**:
   - Select the user → Security Credentials → Create Access Key
   - Choose "Application running outside AWS"
   - Save the Access Key ID and Secret Access Key

5. **Configure TSZ**:
   ```env
   AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
   AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
   AI_PROVIDER=BEDROCK
   AWS_BEDROCK_REGION=us-east-1
   AWS_BEDROCK_MODEL_ID=anthropic.claude-3-sonnet-20240229-v1:0
   ```

##### Option 2: IAM Role (Production - Recommended)

For production deployments on AWS (EC2, ECS, EKS, Lambda):

1. **Create IAM Role**:
   - Go to IAM → Roles → Create Role
   - Select trusted entity: EC2, ECS Task, or EKS
   - Attach the policy: `TSZ-Bedrock-Access`
   - Name: `TSZ-Bedrock-Role`

2. **Attach Role to Service**:
   - **EC2**: Attach role to EC2 instance
   - **ECS**: Specify role in task definition
   - **EKS**: Use IRSA (IAM Roles for Service Accounts)

3. **Configure TSZ** (no credentials needed):
   ```env
   # No AWS_ACCESS_KEY_ID or AWS_SECRET_ACCESS_KEY needed
   # Role credentials are automatically loaded
   AI_PROVIDER=BEDROCK
   AWS_BEDROCK_REGION=us-east-1
   AWS_BEDROCK_MODEL_ID=anthropic.claude-3-sonnet-20240229-v1:0
   ```

##### Option 3: AWS CLI Profile (Local Development)

1. **Configure AWS CLI**:
   ```bash
   aws configure --profile tsz-bedrock
   # Enter your Access Key ID
   # Enter your Secret Access Key
   # Enter default region (e.g., us-east-1)
   ```

2. **Use Profile**:
   ```bash
   AWS_PROFILE=tsz-bedrock docker-compose up -d
   ```

##### Minimum IAM Policy (Specific Model)

For tighter security, restrict to specific models:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "bedrock:InvokeModel"
            ],
            "Resource": [
                "arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-3-sonnet-20240229-v1:0"
            ]
        }
    ]
}
```

#### Supported Model Families

TSZ's Bedrock provider supports the following model families:

- **Anthropic Claude** - Claude 3 Opus, Sonnet, Haiku
- **Amazon Titan** - Titan Text Express, Lite
- **Meta Llama** - Llama 3 (8B, 70B)
- **Mistral** - Mistral 7B, Mixtral 8x7B
- **Cohere** - Command, Command Light
- **OpenAI** - GPT-OSS 20B, GPT-OSS 120B (via Bedrock)

For the latest model IDs and availability, refer to the [AWS Bedrock Model IDs documentation](https://docs.aws.amazon.com/bedrock/latest/userguide/model-ids.html).

#### Features

- ✅ Non-streaming requests
- ✅ Multiple model families
- ✅ AWS IAM authentication
- ✅ VPC endpoint support
- ✅ AWS KMS encryption
- ✅ CloudTrail audit logging
- ⏳ Streaming requests (planned for future release)

#### Limitations

- Streaming is not yet supported (non-streaming only). If a client sends `stream=true` while `AI_PROVIDER=BEDROCK`, TSZ returns an OpenAI-compatible `400` error with code `streaming_not_supported`.
- Requires AWS credentials and permissions
- Model availability varies by region
- Some models require explicit enablement in AWS console

#### Regional Availability

Bedrock is available in the following regions:

- `us-east-1` (N. Virginia)
- `us-west-2` (Oregon)
- `ap-southeast-1` (Singapore)
- `ap-northeast-1` (Tokyo)
- `eu-central-1` (Frankfurt)
- `eu-west-1` (Ireland)
- `eu-west-3` (Paris)

Check [AWS Bedrock documentation](https://docs.aws.amazon.com/bedrock/latest/userguide/what-is-bedrock.html) for the latest regional availability.

#### Security Features

1. **Data Residency:** All data stays within AWS boundaries
2. **Encryption:** 
   - In-transit: TLS 1.2+
   - At-rest: AWS KMS encryption
3. **Access Control:** IAM policies and roles
4. **Audit:** CloudTrail logging for all API calls
5. **Network Isolation:** VPC endpoints for private connectivity

---

## Configuration

### Environment-Based Configuration

TSZ uses environment variables for provider configuration. This allows for:

- Easy configuration management across environments
- Secure credential handling
- No code changes required to switch providers

### Configuration Precedence

1. Environment variables
2. `.env` file (if present)
3. Default values (defined in code)

### Validation

TSZ validates configuration at startup:

- Required parameters are checked
- Credentials are validated (where possible)
- Provider initialization is tested

If configuration is invalid, TSZ will:
- Log detailed error messages
- Fail fast (exit with error code)
- Provide guidance on fixing the issue

---

## Provider Comparison

### Feature Matrix

| Feature | OpenAI-Compatible | AWS Bedrock |
|---------|-------------------|-------------|
| Non-streaming | ✅ | ✅ |
| Streaming | ✅ | ⏳ Planned |
| Multiple models | ✅ | ✅ |
| Custom endpoints | ✅ | ✅ (VPC) |
| Authentication | Bearer token | AWS IAM |
| Encryption | TLS | TLS + KMS |
| Audit logging | Application logs | CloudTrail |
| Cost | Varies by provider | AWS pricing |
| Latency | Depends on endpoint | AWS network |
| Data residency | Depends on provider | AWS regions |

### Use Case Recommendations

#### Use OpenAI-Compatible When:

- You need maximum flexibility in provider choice
- You're using local models (Ollama, LM Studio)
- You have existing OpenAI integrations
- You need streaming support immediately
- You're in development/testing phase

#### Use AWS Bedrock When:

- You need to keep data within AWS boundaries
- You require AWS compliance certifications
- You want to leverage AWS IAM for access control
- You need VPC endpoint connectivity
- You're already using AWS services
- You need CloudTrail audit logging

---

## Migration Guide

### From OpenAI-Compatible to Bedrock

1. **Update Environment Variables:**
   ```env
   # Before
   AI_PROVIDER=OPENAI_COMPATIBLE
   AI_MODEL_URL=https://api.openai.com/v1
   AI_API_KEY=sk-...
   AI_MODEL=gpt-4
   
   # After
   AI_PROVIDER=BEDROCK
   AWS_BEDROCK_REGION=us-east-1
   AWS_BEDROCK_MODEL_ID=anthropic.claude-3-sonnet-20240229-v1:0
   ```

2. **Configure AWS Credentials:**
   ```bash
   aws configure
   # or set environment variables
   export AWS_ACCESS_KEY_ID=...
   export AWS_SECRET_ACCESS_KEY=...
   ```

3. **Verify IAM Permissions:**
   - Ensure your IAM user/role has `bedrock:InvokeModel` permission

4. **Test Configuration:**
   ```bash
   # Restart TSZ
   docker-compose restart api
   
   # Check logs
   docker logs thyris_api
   ```

5. **Update Application Code (if needed):**
   - No code changes required for gateway usage
   - Update model names in requests if needed

### From Bedrock to OpenAI-Compatible

1. **Update Environment Variables:**
   ```env
   # Before
   AI_PROVIDER=BEDROCK
   AWS_BEDROCK_REGION=us-east-1
   AWS_BEDROCK_MODEL_ID=anthropic.claude-3-sonnet-20240229-v1:0
   
   # After
   AI_PROVIDER=OPENAI_COMPATIBLE
   AI_MODEL_URL=https://api.openai.com/v1
   AI_API_KEY=sk-...
   AI_MODEL=gpt-4
   ```

2. **Remove AWS Credentials (optional):**
   - If not using AWS for other services

3. **Test Configuration:**
   ```bash
   docker-compose restart api
   docker logs thyris_api
   ```

---

## Troubleshooting

### Common Issues

#### 1. "Failed to initialize AI provider"

**Symptoms:**
```
2025/12/18 01:26:27 Warning: Failed to initialize AI provider: bedrock region is required
```

**Solutions:**
- Check that all required environment variables are set
- Verify environment variable names are correct
- Ensure `.env` file is in the correct location

#### 2. "Access Denied" (Bedrock)

**Symptoms:**
```
bedrock invoke failed: AccessDeniedException: User is not authorized to perform: bedrock:InvokeModel
```

**Solutions:**
- Verify IAM permissions include `bedrock:InvokeModel`
- Check that the model ARN in the policy matches the model you're using
- Ensure AWS credentials are correctly configured

#### 3. "Model not found" (Bedrock)

**Symptoms:**
```
bedrock invoke failed: ResourceNotFoundException: Could not find model
```

**Solutions:**
- Verify the model ID is correct
- Check that the model is available in your region
- Ensure the model is enabled in AWS Bedrock console

#### 4. "Connection refused" (OpenAI-Compatible)

**Symptoms:**
```
request failed: dial tcp 127.0.0.1:11434: connect: connection refused
```

**Solutions:**
- Verify the endpoint URL is correct
- Check that the service is running
- For Docker: use `host.docker.internal` instead of `localhost`

### Debug Mode

Enable debug logging:

```env
LOG_LEVEL=debug
```

This will provide detailed information about:
- Provider initialization
- Request/response payloads
- Error details

---

## Best Practices

### 1. Environment-Specific Configuration

Use different providers for different environments:

```env
# Development
AI_PROVIDER=OPENAI_COMPATIBLE
AI_MODEL_URL=http://localhost:11434/v1

# Staging
AI_PROVIDER=BEDROCK
AWS_BEDROCK_REGION=us-east-1

# Production
AI_PROVIDER=BEDROCK
AWS_BEDROCK_REGION=us-east-1
AWS_BEDROCK_ENDPOINT_OVERRIDE=https://vpce-xxx...
```

### 2. Credential Management

- **Never commit credentials** to version control
- Use **secret managers** (AWS Secrets Manager, HashiCorp Vault)
- Rotate credentials regularly
- Use **IAM roles** when possible (for Bedrock)

### 3. Cost Optimization

- Use **cheaper models** for development/testing
- Implement **caching** to reduce API calls
- Monitor usage with provider-specific tools
- Consider **local models** (Ollama) for development

### 4. Performance Optimization

- Choose **regions close to your users**
- Use **VPC endpoints** for Bedrock (reduces latency)
- Implement **connection pooling**
- Monitor and optimize **timeout settings**

### 5. Monitoring and Observability

- Log all provider interactions
- Track error rates by provider
- Monitor latency metrics
- Set up alerts for failures

### 6. Disaster Recovery

- Have a **fallback provider** configured
- Test provider switching regularly
- Document the switching process
- Monitor provider health status

---

## Additional Resources

- [TSZ Quick Start Guide](QUICK_START.md)
- [TSZ API Reference](API_REFERENCE.md)
- [AWS Bedrock Documentation](https://docs.aws.amazon.com/bedrock/)
- [OpenAI API Documentation](https://platform.openai.com/docs/api-reference)
- [Ollama Documentation](https://ollama.ai/docs)

---

## Support

For issues or questions about AI providers:

- **GitHub Issues:** https://github.com/thyrisAI/safe-zone/issues
- **Email:** open-source@thyris.ai

For AWS Bedrock-specific issues:
- **AWS Support:** https://aws.amazon.com/support/
- **AWS Bedrock Forum:** https://repost.aws/tags/bedrock
