package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/cloudfoundry-community/go-cfenv"
	aws "github.com/olivere/elastic/aws/v4"
	cspr "github.com/soutenniza/cspr-collector"
)

var (
	NumberOfWorkers          = flag.Int("n", 4, "the number of workers to start")
	HTTPListenHost           = flag.String("host", "127.0.0.1:8080", "address to listen for http requests on")
	OutputStdout             = flag.Bool("output-stdout", false, "enable stdout output")
	OutputHTTPEnabled        = flag.Bool("output-http", false, "enable http output")
	OutputHTTPHost           = flag.String("output-http-host", "http://localhost:80/", "http host to send the csp violations to")
	OutputESEnabled          = flag.Bool("output-es", false, "enable elasticsearch output")
	OutputAWSESEnabled       = flag.Bool("output-aws-es", false, "enable aws elasticsearch output")
	OutputESHost             = flag.String("output-es-host", "http://localhost:9200/", "elasticsearch host to send the csp violations to")
	OutputESIndex            = flag.String("output-es-index", "cspr-violations", "elasticsearch index to save the csp violations in")
	OutputEsCertFile         = flag.String("output-es-cert-file", "", "cert file for elasticsearch")
	OutputEsKeyFile          = flag.String("output-es-key-file", "", "key file for elasticsearch")
	OutputEsCaFile           = flag.String("output-es-ca-file", "", "ca file for elasticsearch")
	OutputAWSESAccessKey     = flag.String("output-aws-es-access-key", "", "access key for elasticsearch")
	OutputAWSESSecretKey     = flag.String("output-aws-es-secret-key", "", "secret key for elasticsearch")
	OutputAWSESRegion        = flag.String("output-aws-es-region", "us-west-1", "secret key for elasticsearch")
	OutputCFAWSESServiceName = flag.String("output-cf-aws-es-name", "", "service name for cf aws elasticsearch")
	OutputCFAWSESEnabled     = flag.Bool("output-cf-aws-es", false, "enable aws elasticsearch to use cloud foundry creds")
)

func main() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	flag.Parse()

	workQueue := make(chan cspr.CSPRequest, 100)

	output := NewOutput()
	dispatcher := cspr.NewDispatcher(*NumberOfWorkers, output, workQueue)
	dispatcher.Run()

	collector := cspr.NewCollector(workQueue)
	server := &http.Server{Addr: *HTTPListenHost, Handler: collector}

	go func() {
		log.Printf("HTTP server listening on %s.", *HTTPListenHost)
		if err := server.ListenAndServe(); err != nil {
			log.Print(err.Error())
		}
	}()

	<-stop

	log.Print("Shutting down the server.")
	err := server.Shutdown(context.Background())
	if err != nil {
		log.Fatal(err)
		return
	}
	log.Println("Server gracefully stopped.")
}

func NewOutput() *cspr.CombinedOutput {
	var outputs []cspr.Output

	if *OutputStdout {
		log.Printf("Enable Stdout Output.")
		outputs = append(outputs, &cspr.StdoutOutput{})
	}

	if *OutputHTTPEnabled {
		log.Printf("Enable HTTP Output.")
		outputs = append(outputs, &cspr.HTTPOutput{Url: *OutputHTTPHost})
	}

	if *OutputESEnabled {
		log.Printf("Enable ES Output.")
		outputs = append(outputs, &cspr.ElasticsearchOutput{
			Url:    *OutputESHost,
			Index:  *OutputESIndex,
			Client: cspr.NewHttpClient(*OutputEsCertFile, *OutputEsKeyFile, *OutputEsCaFile),
		})
	}

	if *OutputAWSESEnabled {
		log.Printf("Enable AWS ES Output.")

		signingClient := aws.NewV4SigningClient(credentials.NewStaticCredentials(
			*OutputAWSESAccessKey,
			*OutputAWSESSecretKey,
			"",
		), *OutputAWSESRegion)

		outputs = append(outputs, &cspr.ElasticsearchOutput{
			Url:    "https://" + *OutputESHost,
			Index:  *OutputESIndex,
			Client: signingClient,
		})
	}

	if *OutputCFAWSESEnabled {
		log.Printf("Enable CF AWS ES Output.")

		appEnv, _ := cfenv.Current()
		serviceES, _ := appEnv.Services.WithName(*OutputCFAWSESServiceName)

		signingClient := aws.NewV4SigningClient(credentials.NewStaticCredentials(
			fmt.Sprintf("%v", serviceES.Credentials["access_key"]),
			fmt.Sprintf("%v", serviceES.Credentials["secret_key"]),
			"",
		), *OutputAWSESRegion)

		outputs = append(outputs, &cspr.ElasticsearchOutput{
			Url:    "https://" + fmt.Sprintf("%v", serviceES.Credentials["host"]),
			Index:  *OutputESIndex,
			Client: signingClient,
		})
	}

	return &cspr.CombinedOutput{Outputs: outputs}
}
