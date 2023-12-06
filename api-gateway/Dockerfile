FROM golang:1.21.0 AS builder
RUN go env -w GOPROXY=direct
WORKDIR /app
COPY . .
RUN go mod download
RUN go mod tidy
# Do not forget to set CGO_ENABLED=0
# https://teitei-tk.hatenablog.com/entry/2020/07/24/120000
RUN CGO_ENABLED=0 go build -o api ./main.go

# Use distroless image
# https://cohalz.co/entry/2021/12/11/000000
FROM gcr.io/distroless/static:nonroot
COPY --from=builder --chown=nonroot:nonroot /app/api /usr/local/bin/api
EXPOSE 8080
CMD [ "/usr/local/bin/api"]
