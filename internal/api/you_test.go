package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestYouDecodesBareObjectAndDataEnvelope(t *testing.T) {
	for name, body := range map[string]string{
		"bare": `{"publicId":"pe-abc123","email":"ada@example.com","name":"Ada","next":{"step":"connect_git","instruction":"Connect GitHub","url":"https://dash.example/git"},"optional":[{"step":"ssh_key","done":false,"instruction":"Add an SSH key","url":"https://dash.example/ssh"}]}`,
		"data": `{"data":{"publicId":"pe-abc123","email":"ada@example.com","name":"Ada","next":{"step":"connect_git","instruction":"Connect GitHub","url":"https://dash.example/git"},"optional":[{"step":"ssh_key","done":false,"instruction":"Add an SSH key","url":"https://dash.example/ssh"}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/v1/you" {
					t.Errorf("path = %q", request.URL.Path)
				}
				if request.Header.Get("Accept") != "application/json" {
					t.Errorf("accept = %q", request.Header.Get("Accept"))
				}
				_, _ = writer.Write([]byte(body))
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "frbit-at-test", server.Client(), "frbit/test")
			if err != nil {
				t.Fatal(err)
			}

			response, err := client.You(context.Background())
			if err != nil {
				t.Fatalf("You() error = %v", err)
			}
			you := response.You
			if you.Email != "ada@example.com" || you.Next == nil || you.Next.Step != "connect_git" || you.Next.URL == nil || len(you.Optional) != 1 {
				t.Fatalf("you = %#v", you)
			}
			if string(response.Next) != `{"step":"connect_git","instruction":"Connect GitHub","url":"https://dash.example/git"}` {
				t.Fatalf("raw next = %s", response.Next)
			}
		})
	}
}
