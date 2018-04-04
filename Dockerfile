FROM golang
ADD . /go/src/github.com/abacusresearch/android-publisher-bot
RUN go get github.com/abacusresearch/android-publisher-bot/...
RUN go install github.com/abacusresearch/android-publisher-bot
ENTRYPOINT /go/bin/android-publisher-bot
