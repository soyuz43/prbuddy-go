# Makefile
.PHONY: r
r:
	./scripts/rebuild.sh

.PHONY: pr
pr:
	prbuddy-go pr create