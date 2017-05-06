FROM nimmis/alpine-golang
ADD main /main
EXPOSE 12345
ENTRYPOINT ["/main"]
