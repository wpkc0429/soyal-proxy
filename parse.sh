#!/bin/bash
cd /var/www/docker/html/go-lang/soyal-proxy
docker run --rm -v /var/www/docker/html/go-lang/soyal-proxy:/work python:3.9-slim bash -c "pip install pypdf && cd /work && python read_pdf.py 'Protocol_881E_725Ev2_82xEv5 4V04.pdf'"
