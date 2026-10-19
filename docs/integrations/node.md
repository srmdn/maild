# Send Email from Node.js

This example sends a plain-text transactional email through a running maild instance. It requires Node.js 18 or later for the built-in `fetch` API.

Set the maild base URL and an API key before running it:

```sh
export MAILD_BASE_URL=http://localhost:8080
export MAILD_API_KEY=your-personal-api-key
```

```js
async function main() {
  const response = await fetch(`${process.env.MAILD_BASE_URL}/v1/messages`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-API-Key": process.env.MAILD_API_KEY,
    },
    body: JSON.stringify({
      from_email: "noreply@example.com",
      to_email: "customer@example.com",
      subject: "Your receipt",
      body_text: "Thanks for your order.",
    }),
  });

  const body = await response.text();
  if (response.status !== 201) {
    throw new Error(`maild returned ${response.status}: ${body}`);
  }

  console.log(body);
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
```

`201 Created` means maild accepted and queued the message. It does not guarantee delivery; use `GET /v1/messages/logs` or `GET /v1/messages/timeline?message_id=<id>` to inspect delivery after the worker runs. A personal API key scopes requests to its workspace, so these examples omit `workspace_id`.

maild currently sends a single plain-text recipient per request. It does not support HTML bodies, attachments, CC/BCC, or SMTP submission.
