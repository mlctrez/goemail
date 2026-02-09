# goemail

### Summary
`goemail` is an AWS Lambda function written in Go that handles SES inbound emails. It forwards emails arriving at any address on your domain to a designated target email address (e.g., your Gmail). It includes features for tracking original recipients and blocking unwanted senders/recipients via an S3-backed blocklist.

### Features
*   **Email Forwarding**: Automatically forwards inbound SES emails to a configured `EMAIL_TO` address.
*   **Original Address Reflection**: Adds `X-Original-To` and `X-Original-From` headers to forwarded emails. This allows you to see the original recipient in your inbox, which is useful for identifying which alias received the email.
*   **S3-Based Blocklist**: Prevents forwarding of emails sent to addresses listed in a `blocks.txt` file stored in S3.
*   **Remote Blocklist Management**: Add addresses to the blocklist by sending an email:
    *   **From**: Your configured `EMAIL_TO` address.
    *   **Subject**: `block` (case-insensitive).
    *   **To**: The address(es) you wish to block.
    *   **Result**: The system updates the blocklist and sends a confirmation email.
*   **Robust Header Handling**: Correctly handles multi-line (folded) headers and performs normalization of email addresses for reliable matching.
*   **Error Handling**: Provides detailed logging and sends administrative alerts if forwarding fails, including pre-signed S3 links for manual retrieval.

### Build
The project uses [Mage](https://magefile.org/) for build and deployment automation.

*   **Build**: `go build .`
*   **Test**: `go test ./...`
*   **Deploy**: `mage deploy` (requires AWS credentials and a `.env` file).

### Setup
1.  **Environment Configuration**: Create a `.env` file in the root directory with the following variables:
    *   `EMAIL_BUCKET`: The S3 bucket where SES stores inbound emails.
    *   `EMAIL_FROM`: The address that will appear in the `From` header of forwarded emails (must be a verified identity in SES).
    *   `EMAIL_TO`: Your target destination email address.
    *   `GO_LAMBDA_NAME`: (Optional) The name for the Lambda function.
2.  **AWS Infrastructure**:
    *   The `mage deploy` command handles the creation/update of the Lambda function and its IAM role.
    *   The IAM role is automatically granted `AmazonS3FullAccess`, `AmazonSESFullAccess`, and `AWSLambdaBasicExecutionRole` permissions.
    *   You will need to configure SES Receipt Rules to store incoming emails in the S3 bucket and trigger this Lambda function.
3.  **SES Configuration**:
    *   **Identities**: Verify your domain and the `EMAIL_TO` recipient address in the SES "Identities" section.
    *   **MX Records**: Configure your domain's DNS with MX records pointing to the AWS SES receiving endpoint (e.g., `10 inbound-smtp.us-east-1.amazonaws.com`).
    *   **Receipt Rule Set**: Create a default rule set with a rule that includes:
        1.  **S3 Action**: Delivers the raw email to the bucket specified in `EMAIL_BUCKET`.
        2.  **Lambda Action**: Invokes the `goemail` Lambda function using the "RequestResponse" invocation type.
    *   **Permissions**: Ensure that the S3 bucket policy allows SES to write to it and the Lambda function has a resource-based policy allowing `ses.amazonaws.com` to invoke it (handled automatically by `mage deploy`).

[![Go Report Card](https://goreportcard.com/badge/github.com/mlctrez/goemail)](https://goreportcard.com/report/github.com/mlctrez/goemail)

created by [tigwen](https://github.com/mlctrez/tigwen)
