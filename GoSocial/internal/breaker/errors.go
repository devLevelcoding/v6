package breaker

import "errors"

// ErrCircuitOpen is returned when a call is rejected because the circuit is
// Open and its timeout hasn't elapsed yet.
var ErrCircuitOpen = errors.New("circuit breaker is open")
