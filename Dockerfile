FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/perfumebot ./cmd/perfumebot

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/perfumebot /perfumebot
VOLUME ["/data"]
ENV DB_PATH=/data/perfumes.db
HEALTHCHECK --interval=30s --timeout=3s --retries=3 CMD ["/perfumebot", "healthcheck"]
ENTRYPOINT ["/perfumebot"]
