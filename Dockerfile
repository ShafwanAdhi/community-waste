FROM golang:1.27.2-bookworm AS development
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
CMD ["go", "test", "-race", "-count=1", "./..."]

FROM development AS build
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/community-waste ./cmd/api

FROM debian:bookworm-slim AS runtime
RUN groupadd --gid 10001 app \
    && useradd --uid 10001 --gid app --no-create-home --shell /usr/sbin/nologin app \
    && mkdir -p /app/uploads \
    && chown app:app /app/uploads
WORKDIR /app
COPY --from=build /out/community-waste /app/community-waste
USER app
EXPOSE 8080
ENTRYPOINT ["/app/community-waste"]
CMD ["serve"]
