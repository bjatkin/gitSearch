# Git Search
this is a small http service that allows for simple searching of code in the main brach of configured git repos

# How To Deploy
this is a simple go http server which can be deployed like any other server.
By default this service runs on port 8000 but this can be configured in the config.yaml file
you must also list the targe git repos for the service to search.
Once you have configured the port and the repos you can deploy the service by running
```go run . [config_file]```
for the directory where this code is located.
You can also build this code by running ```go build . -o git_search``` and then deploy
the compiled applicaiton by runnign ```./git_search [config_file]```
see the config.yaml file in this project for an example configuration.