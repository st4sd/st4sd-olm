# Deploy ST4SD using OperatorLifecycleManager (OLM)

This repository contains the code to build the [Operator](https://operatorframework.io/) for deploying the [Simulation Toolkit for Scientific Discovery (ST4SD)](https://github.ibm.com/st4sd/overview).
The ST4SD-Runtime is a python framework, and associated services, for creating and deploying virtual-experiments - data-flows which embody the measurement of properties of systems.

There are three parts to the ST4SD-Runtime
- [`st4sd-runtime-core`](https://github.ibm.com/st4sd/st4sd-runtime-core): The core python framework for describing and executing virtual experiments
- [`st4sd-runtime-k8s`](https://github.ibm.com/st4sd/st4sd-runtime-k8s): Extensions which enable running and managing virtual-experiments on k8s clusters  
- [`st4sd-runtime-service`](https://github.ibm.com/st4sd/st4sd-runtime-service): A RESTapi based service allowing users to add, start, stop and query virtual-experiments

## Features

* Cross-platform data-flows
  * Supports multiple backends  (LSF, OpenShift/Kubernetes, local)
  * Abstracts differences between backends allowing a single component description to be used
  * Variables can be used to encapsulate platform specific options
  * Can define component and platform specific environments
* Co-processing model
  * Consumers can be configured to run repeatedly while their producers are alive
* Simple to replicate workflow sub-graphs over sets of inputs 
* Supports `do-while` constructs
* Handles task persistence across backend allocation windows and allows user customisable restarts
* Deploy workflows directly from github (Kubernetes stack)
* Store and retrieve data and metadata from st4sd-datastore

## More Information

Our [documentation website](https://pages.github.ibm.com/overview) contains detailed information on installing ST4SD, 
writing and running virtual-experiments, along with much more. 
