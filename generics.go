package relay

import (
	"bufio"
	"encoding/json"
)

// ExecuteAs executes req and deserializes the JSON response body directly into
// a value of type T. It is equivalent to calling [Client.ExecuteJSON] but
// avoids the need for an explicit interface{} target.
//
// Example:
//
//	type User struct { ID int; Name string }
//	user, resp, err := relay.ExecuteAs[User](client, client.Get("/users/1"))
func ExecuteAs[T any](c *Client, req *Request) (T, *Response, error) {
	var zero T
	resp, err := c.Execute(req)
	if err != nil {
		return zero, nil, err
	}
	var out T
	if jsonErr := resp.JSON(&out); jsonErr != nil {
		return zero, resp, jsonErr
	}
	return out, resp, nil
}

// ExecuteAsStream sends req and decodes the response body as newline-delimited
// JSON (JSONL / NDJSON), invoking handler for each decoded value of type T.
//
// The stream is consumed lazily — each line is decoded on demand rather than
// buffering the entire response. handler is called synchronously in the
// goroutine that called ExecuteAsStream.
//
// Return false from handler to stop reading and close the connection early.
// ExecuteAsStream returns when handler returns false, the stream ends, or an
// I/O or decoding error occurs.
//
// Like [Client.ExecuteStream], no retry logic is applied and the connection is
// closed automatically when ExecuteAsStream returns.
//
// Example:
//
//	type LogLine struct { Level string; Message string }
//	err := relay.ExecuteAsStream[LogLine](client, client.Get("/logs"), func(l LogLine) bool {
//	    fmt.Println(l.Level, l.Message)
//	    return true
//	})
func ExecuteAsStream[T any](c *Client, req *Request, handler func(T) bool) error {
	stream, err := c.ExecuteStream(req)
	if err != nil {
		return err
	}
	defer stream.Body.Close()

	scanner := bufio.NewScanner(stream.Body)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue // skip blank lines between records
		}
		var item T
		if jsonErr := json.Unmarshal(line, &item); jsonErr != nil {
			return jsonErr
		}
		if !handler(item) {
			return nil
		}
	}
	return scanner.Err()
}
