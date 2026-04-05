package relay

import (
	"bufio"
	"encoding/json"
)

// ExecuteAs executes req and deserialises the response body into a value of
// type T. When a [WithResponseDecoder] is configured on the client it is used;
// otherwise Decode falls back to JSON for application/json content and XML for
// application/xml. It is equivalent to calling [Client.Execute] followed by
// [Response.Decode] but avoids the need for an explicit interface{} target.
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
	if decErr := resp.Decode(&out); decErr != nil {
		return zero, resp, decErr
	}
	return out, resp, nil
}

// ExecuteAsStream sends req and decodes the response body as newline-delimited
// JSON (JSONL / NDJSON), invoking handler for each decoded value of type T.
//
// The stream is consumed lazily - each line is decoded on demand rather than
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
	defer func() { _ = stream.Body.Close() }()

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

// DecodeJSON decodes the response body as JSON into a value of type T without
// requiring a pre-allocated target. It is the typed equivalent of calling
// [Response.JSON] on a freshly allocated pointer.
//
// user, err := relay.DecodeJSON[User](resp)
func DecodeJSON[T any](resp *Response) (T, error) {
	var v T
	err := json.Unmarshal(resp.Body(), &v)
	return v, err
}

// DecodeXML decodes the response body as XML into a value of type T.
//
// envelope, err := relay.DecodeXML[SOAPEnvelope](resp)
func DecodeXML[T any](resp *Response) (T, error) {
	var v T
	err := resp.XML(&v)
	return v, err
}

// DecodeAs decodes the response body into a value of type T using the
// response's content-type-aware decoder. When a [WithResponseDecoder] has
// been configured on the client, it is used; otherwise the method falls back
// to JSON for application/json content and XML for application/xml.
//
// Use DecodeAs when you fetch the response manually and decode it separately,
// or when you want content-type-driven dispatch without specifying the format.
//
// order, err := relay.DecodeAs[Order](resp)
func DecodeAs[T any](resp *Response) (T, error) {
	var v T
	err := resp.Decode(&v)
	return v, err
}
