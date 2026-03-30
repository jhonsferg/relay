package relay

// ExecuteAs executes req and deserialises the JSON response body directly into
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
