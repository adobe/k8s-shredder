FROM alpine AS show_me_your_security
# Install the Certificate-Authority certificates for the app to be able to make
# calls to HTTPS endpoints.
RUN apk add --no-cache ca-certificates

# The second stage, create a small final image
FROM scratch
# Copy the /etc/passwd file we created in the builder stage. This creates a new
# non-root user as a security best practice.
COPY --from=show_me_your_security /etc/passwd /etc/passwd
# Copy the certs from the builder stage
COPY --from=show_me_your_security /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# copy our binary
COPY k8s-shredder /k8s-shredder
ENTRYPOINT ["/k8s-shredder"]
