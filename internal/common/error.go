package common

type RequestErrorMessage struct {
	Time    string `json:"time"`
	Request string `json:"request"`
	Code    int    `json:"code"`
	Error   string `json:"error"`
}
