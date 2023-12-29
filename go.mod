module impact

go 1.21

require internal/request v1.0.0

replace internal/request => ./internal/request

require internal/message v1.0.0

replace internal/message => ./internal/message

require github.com/blanu/radiowave v0.0.10
