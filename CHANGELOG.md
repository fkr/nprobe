# CHANGELOG

## 0.4.0 (tbd)

- Add goreleaser tooling
- CRUD via RESTful interface for satellites
- add generic logging middleware
- better use of chi middleware
- rework URLs to be more restful - #16
- avoid unecessary work in retry loops
- if a satellite has no targets configured, send error upon target retrieval
- if no authorization is configured print a warning to STDOUT
- error handling for dns resolution
- Update pro-bing to 0.4.1
- Proper mutexes around datastructures in head node
- Dockerfile switch to multi-stage. Means _much_ smaller runtime images due to them being based on alpine
- Configuration can be updated at runtime by submitting a configuration through PUT to /config - #27
- Allow to retrieve config via GET to /config
- Clients detects if head node has a new version and terminates itself
- Use constants for header names
- The /healthz endpoint now works if running in 'no database mode'
- Switch gorilla/mux for go-chi/chi5 since gorilla/mux is no longer maintained
- Listen ip and port is now configurable via config file
- If satellite can't submit its data if head node is not reachable the data is now buffered - #1
- Submission of probe results is handled via own go routine - #23

## 0.3.0 (2022-10-19) and earlier

- lot's of ground work

