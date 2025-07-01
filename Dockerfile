FROM gcr.io/distroless/static-debian11:nonroot
ENTRYPOINT ["/baton-eks"]
COPY baton-eks /