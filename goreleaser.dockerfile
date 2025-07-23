FROM alpine
COPY catwalk /usr/bin/catwalk
EXPOSE 8080
CMD ["/usr/bin/catwalk"]
