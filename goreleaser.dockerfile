FROM alpine
COPY fur /usr/bin/fur
EXPOSE 8080
CMD ["/usr/bin/fur"]
