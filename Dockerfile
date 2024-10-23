# Use the official Golang 1.23 image as the base image
FROM golang:1.23 as builder

# Set the current working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum files first to utilize Docker caching
COPY go.mod go.sum ./

# Download necessary Go modules
RUN go mod download

# Copy the rest of the application files
COPY . .

# Build the Go app
RUN CGO_ENABLED=0 GOOS=linux go build -o website-logo-api .

# Use a minimal base image to reduce the image size
FROM gcr.io/distroless/base-debian10

# Set the working directory inside the container
WORKDIR /

# Copy the binary from the builder stage
COPY --from=builder /app/website-logo-api .

# Expose port 8080
EXPOSE 8080

# Command to run the application
CMD ["/website-logo-api"]
