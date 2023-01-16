# Deploy ST4SD using OperatorLifecycleManager (OLM)

This repository contains the code to build the [Operator](https://operatorframework.io/) for deploying the [Simulation Toolkit for Scientific Discovery (ST4SD)](https://github.ibm.com/st4sd/overview).
The ST4SD-Runtime is a python framework, and associated services, for creating and deploying virtual-experiments - data-flows which embody the measurement of properties of systems.

There are three parts to the ST4SD-Runtime
- [`st4sd-runtime-core`](https://github.com/st4sd/st4sd-runtime-core): The core python framework for describing and executing virtual experiments
- [`st4sd-runtime-k8s`](https://github.com/st4sd/st4sd-runtime-k8s): Extensions which enable running and managing virtual-experiments on k8s clusters  
- [`st4sd-runtime-service`](https://github.com/st4sd/st4sd-runtime-service): A RESTapi based service allowing users to add, start, stop and query virtual-experiments

## Features

* Cross-platform data-flows
  * Supports multiple backends  (LSF, OpenShift/Kubernetes, local)
  * Abstracts the differences between backends allowing a single component description to be used
  * Variables can be used to encapsulate platform specific options
  * Can define component and platform specific environments
* Co-processing model
  * Consumers can be configured to run repeatedly while their producers are alive
* Simple to replicate workflow sub-graphs over sets of inputs 
* Supports `do-while` constructs
* Handles task persistence across backend allocation windows and allows user customisable restarts
* Deploy workflows directly from GitHub (Kubernetes stack)
* Store and retrieve data and metadata from st4sd-datastore

## More Information

Our [documentation website](https://pages.github.ibm.com/overview) contains detailed information on installing ST4SD, 
writing and running virtual-experiments, along with much more. 


## Installation

You can install ST4SD by first installing this operator in your cluster and then asking it to deploy ST4SD under a namespace in your cluster.

### Install st4sd-olm (this OLM operator) in your cluster

Requirements:

1. **Access to an OpenShift cluster with `cluster-admin` permissions**
    - Required for creation of a kubernetes objects (such as CustomResourceDefinition and Role Based Access Control (RBAC)).
2. **OpenShift command line tools  (`oc` v4.9+)**
    - Instructions: <https://docs.openshift.com/container-platform/4.9/cli_reference/openshift_cli/getting-started-cli.html>
    - Install stable version of`oc` from <https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable/>
    - It is good practice to periodically update your `oc` command line utility to ensure that your `oc` binary contains the latest patches and bug-fixes.

Steps:

1. Clone the repository, and cd into the `st4sd-olm` directory.
2. Login to your OpenShift web console. Also log in on a terminal to your OpenShift cluster using the `oc` command-line interface.
3. In your terminal, use oc to apply the [`deploy.yaml`](examples/deploy.yaml) objects (e.g. `oc apply -f deploy/deploy.yaml`)
4. You should see a new pod named `st4sd-catalog-XXXX` in the namespace `openshift-marketplace`.
5. Wait for the pod to transition to the `Running` state and then wait for 30 more seconds.
6. Switch back to the OpenShift Web Console page on your browser. Make sure you are in the `Administrator` view. 
7. On the left panel, expand the menu `Operators`, and then click the `OperatorHub` option.
8. Wait for the panel on the right to refresh.
9. In the search box type `ST4SD`. The right panel should filter out unrelated entries and display one with the label 
   `Simulation Toolkit For Scientific Discovery (ST4SD)`. 
   If you do not see the ST4SD operator this means that OpenShift is still decoding the information from step 3 - 
   wait for 30 seconds and repeat steps 8 and 9.
10. Click on the `Simulation Toolkit For Scientific Discovery (ST4SD)` entry and wait for a new panel to pop up. 
    Click the `Install` button at the top left of this panel - you will transition to a new page.
    If the button label is `Uninstall`, then the operator is already installed on your cluster. 
    In this case, you do not need to re-install the operator - skip to step 13.
11. In the new page, select the `stable` update channel. Set the `Installed Namespace` dropdown to `openshift-operators`. 
    Set `Update approval` to `Automatic` if you wish that ST4SD deployments you create via this operator to be auto-updated. 
    Set it to `Manual` if you wish to manually update this operator and therefore control when you receive new updates to 
    your ST4SD instance. We recommend using the option `Automatic`. Finally, click the `Install` button at the bottom left
    - you will transition to a new page.
12. The page reports the installation status of the operator. Wait for it to become `Installed operator - ready for use`.
13. Verify that the `st4sd-olm` operator pod is up and running. You should see it in the namespace that you deployed the
    operator in Step 11 (e.g. `openshift-operators`).

If you followed the steps above you are now able to install ST4SD in any namespace on your cluster using the 
`st4sd-olm` operator (this repository). You can enable other users of your Cluster to install `st4sd` in their own
namespaces by granting them  Role-Based Access Control (RBAC) privileges to handle 
`simulationtoolkits.deploy.st4sd.ibm.com` objects in their namespaces. 
See [example-role.yaml](examples/example-role.yaml) for an example `Role` object you can use (with a RoleBinding) to 
give users permissions to deploy ST4SD in a namespace. 

**Note**: Update the `metadata.namespace` field in [example-role.yaml](examples/example-role.yaml) to the name of 
the namespace that you wish for users to deploy ST4Sd to. Then `oc apply -f examples/example-role.yaml`. Now, you can 
create a RoleBinding object in the same namespace and use the RoleBinding to map users to this new Role. These users
will be able to install ST4SD using `st4sd-olm` in **this** namespace.

### Install ST4SD using st4sd-olm

The steps below assume that you:

1. have already installed `st4sd-olm` on your cluster using the instructions above
2. have already used `oc` to login to your cluster
3. have Role-Based Access Control (RBAC) permissions to create/modify objects of type `simulationtoolkits.deploy.st4sd.ibm.com` 
   in the namespace that you wish to deploy ST4SD. See above instructions for more information.
    

Steps to install ST4SD in your namespace using `st4sd-olm`:

1. Create the 3 PVCs following the [st4sd-deployment instructions](https://github.com/st4sd/st4sd-deployment/blob/main/docs/install-requirements.md#storage-setup).
   - The PersistentVolumeClaim (PVC) object you create for the field `spec.setup.pvcInstances` should support mounting 
     under multiple pods in Read/Write, filesystem mode (i.e. ReadWriteMany).
   - The PVC you create for the field `pvcDatastore` should support mounting under multiple pods in Read/Write, 
     filesystem mode (i.e. ReadWriteMany).
   - The PVC you create for the field `pvcRuntimeService` should support mounting under multiple pods in Read/Write, 
     filesystem mode (i.e. ReadWriteMany).
2. Modify the [basic.yaml](examples/basic.yaml) YAML file to add the names of the PVCs and the desired RouteDomain of your ST4SD instance
    - If you are using an OpenShift cluster on IBM Cloud, `st4sd-olm` can auto-detect your cluster ingress. 
      You can use `${CLUSTER_INGRESS}` to reference it in your `spec.setup.routeDomain` field (see example). 
3. Create the [basic.yaml](examples/basic.yaml) YAML file (e.g. `oc apply -f examples/basic.yaml`).
