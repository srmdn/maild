# Send Email from Go

This example sends a plain-text transactional email through a running maild instance. It uses only the Go standard library.

Set the maild base URL and an API key before running it:

```sh
export MAILD_BASE_URL=http://localhost:8080
export MAILD_API_KEY=your-personal-api-key
```

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type sendMessageRequest struct {
	FromEmail string `json:"from_email"`
	ToEmail   string `json:"to_email"`
	Subject   string `json:"subject"`
	BodyText  string `json:"body_text"`
}

func main() {
	requestBody, err := json.Marshal(sendMessageRequest{
		FromEmail:   "noreply@example.com",
		ToEmail:     "customer@example.com",
		Subject:     "Your receipt",
		BodyText:    "Thanks for your order.",
	})
	if err != nil {
		panic(err)
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		os.Getenv("MAILD_BASE_URL")+"/v1/messages",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", os.Getenv("MAILD_API_KEY"))

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
	if response.StatusCode != http.StatusCreated {
		panic(fmt.Sprintf("maild returned %s: %s", response.Status, body))
	}

	fmt.Println(string(body))
}
```

`201 Created` means maild accepted and queued the message. It does not guarantee delivery; use `GET /v1/messages/logs` or `GET /v1/messages/timeline?message_id=<id>` to inspect delivery after the worker runs. A personal API key scopes requests to its workspace, so these examples omit `workspace_id`.

maild currently sends a single plain-text recipient per request. It does not support HTML bodies, attachments, CC/BCC, or SMTP submission.
