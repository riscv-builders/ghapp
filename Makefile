TARGETS := web manager migrate


build:
	@for target in $(TARGETS); do		\
		go build -o bin/rvb-$${target}	\
			./cmd/$${target};	\
	done

lint:
	staticcheck ./...

clean:
	rm -f bin/*
