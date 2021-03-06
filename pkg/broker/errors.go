package broker

import (
	"net/http"

	"gitlab.bj.sensetime.com/service-providers/go-open-service-broker-client/v2"
)

var (
	asyncRequiredError = newAsyncRequiredError()
	//concurrencyError   = newConcurrencyError()
)

func newAsyncRequiredError() v2.HTTPStatusCodeError {
	return v2.HTTPStatusCodeError{
		StatusCode:   http.StatusUnprocessableEntity,
		ErrorMessage: &[]string{v2.AsyncErrorMessage}[0],
		Description:  &[]string{v2.AsyncErrorDescription}[0],
	}
}

//func VjjnewConcurrencyError() v2.HTTPStatusCodeError {
//	return v2.HTTPStatusCodeError{
//		StatusCode:   http.StatusUnprocessableEntity,
//		ErrorMessage: &[]string{v2.ConcurrencyErrorMessage}[0],
//		Description:  &[]string{v2.ConcurrencyErrorDescription}[0],
//	}
//}
