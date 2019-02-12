TEMPLATE_FILE=template.yml
OUTPUT_FILE=sam.yml
FUNCTIONS=build/main

build/helper: helper/*.go
	go build -o build/helper ./helper/

build/main: ./*.go
	env GOARCH=amd64 GOOS=linux go build -o build/main .

clean:
	rm $(FUNCTIONS)

test:
	go test -v ./lib/

sam.yml: $(TEMPLATE_FILE) $(FUNCTIONS) build/helper
	aws cloudformation package \
		--region $(shell ./build/helper get Region) \
		--template-file $(TEMPLATE_FILE) \
		--s3-bucket $(shell ./build/helper get CodeS3Bucket) \
		--s3-prefix $(shell ./build/helper get CodeS3Prefix) \
		--output-template-file $(OUTPUT_FILE)

deploy: $(OUTPUT_FILE) build/helper
	aws cloudformation deploy \
		--region $(shell ./build/helper get Region) \
		--template-file $(OUTPUT_FILE) \
		--stack-name $(shell ./build/helper get StackName) \
		--capabilities CAPABILITY_IAM $(shell ./build/helper mkparam)
