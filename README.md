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

You can install ST4SD by first installing this operator in your cluster, configuring it, and then asking it to deploy [ST4SD Cloud](https://st4sd.github.io/overview/st4sd-cloud-getting-started) under a namespace in your cluster.


### Requirements:

1. **Access to an OpenShift cluster with `cluster-admin` permissions**
    - Required for creation of a kubernetes objects (such as CustomResourceDefinition and Role Based Access Control (RBAC)).
2. **OpenShift command line tools  (`oc` v4.9+)**
    - Instructions: <https://docs.openshift.com/container-platform/4.9/cli_reference/openshift_cli/getting-started-cli.html>
    - Install stable version of`oc` from <https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable/>
    - It is good practice to periodically update your `oc` command line utility to ensure that your `oc` binary contains the latest patches and bug-fixes.

Before you continue any further, ensure that you have logged in your OpenShift cluster using the `oc` command-line interface.

### Install the operator

Add the **Simulation Toolkit for Scientific Discovery (ST4SD)** operator to the Operator Catalog of your OpenShift cluster:

```
oc apply -f https://raw.githubusercontent.com/st4sd/st4sd-olm/main/examples/deploy.yaml
```

Then wait for the pod `st4sd-catalog-xxxx` in the namespace `openshift-marketplace` to transition to the `Running` state.


### Configure the operator

Navigate to the OperatorHub in the OpenShift Web Console of your OpenShift cluster and install the operator `Simulation Toolkit for Scientific Discovery (ST4SD)` in the `openshift-operators` namespace.

<details>

<summary>Expand to see step-by-step instructions</summary> 

1. Switch back to the OpenShift Web Console page on your browser. Make sure you are in the `Administrator` view. 
2. On the left panel, expand the menu `Operators`, and then click the `OperatorHub` option.
3. In the search box type `ST4SD`. The right panel should filter out unrelated entries and display one with the label 
   `Simulation Toolkit For Scientific Discovery (ST4SD)`.
4. Click on the `Simulation Toolkit For Scientific Discovery (ST4SD)` entry and wait for a new panel to pop up. 
    Click the `Install` button at the top left of this panel - you will transition to a new page.
    If the button label is `Uninstall`, then the operator is already installed on your cluster. 
    In this case, you do not need to re-install the operator, you may skip to the last step.
5. In the new page, select the `stable` update channel. Set the `Installed Namespace` dropdown to `openshift-operators`. 
    Set `Update approval` to `Automatic` if you wish that ST4SD deployments you create via this operator to be auto-updated. 
    Set it to `Manual` if you wish to manually update this operator and therefore control when you receive new updates to 
    your ST4SD instance. We recommend using the option `Automatic`. Finally, click the `Install` button at the bottom left
    - you will transition to a new page. The page reports the installation status of the operator. Wait for it to become `Installed operator - ready for use`.
6. Verify that the `st4sd-olm` operator pod is up and running. You should see it in the namespace that you deployed the
    operator in Step 11 (e.g. `openshift-operators`).

</details>

<br>

If you followed the above, and you are a cluster-admin, you are now able to install ST4SD in any namespace on your cluster using the 
`st4sd-olm` operator (this repository). You can enable other users of your Cluster to install `st4sd` in their own
namespaces by granting them  Role-Based Access Control (RBAC) privileges to handle 
`simulationtoolkits.deploy.st4sd.ibm.com` objects in their namespaces. You can find more information about this [in our documentation website](https://st4sd.github.io/overview/cloud-manage-users).


See [example-role.yaml](examples/example-role.yaml) for an example `Role` object you can use (with a RoleBinding) to 
give users permissions to deploy ST4SD in a namespace. 

> **Note**: Update the `metadata.namespace` field in [example-role.yaml](examples/example-role.yaml) to the name of 
the namespace that you wish for users to deploy ST4Sd to. Then `oc apply -f examples/example-role.yaml`. Now, you can 
create a RoleBinding object in the same namespace and use the RoleBinding to map users to this new Role. These users
will be able to install ST4SD using `st4sd-olm` in **this** namespace.

### Deploy ST4SD Cloud using the operator

1. Create the 3 PVCs following the [st4sd-deployment instructions](https://github.com/st4sd/st4sd-deployment/blob/main/docs/install-requirements.md#storage-setup). In short create 1 PVC for each of the 3 fields in the [basic.yaml](examples/basic.yaml):
   - `spec.setup.pvcInstances`: The PersistentVolumeClaim (PVC) (e.g. `workflow-instances`) should support mounting 
     under multiple pods in Read/Write, filesystem mode (i.e. ReadWriteMany). ST4SD stores the outputs of components on this PVC.
   - `spec.setup.pvcDatastore`: This PVC (e.g. `datastore-mongodb`) should support mounting on multiple pods in Read/Write, 
     filesystem mode (i.e. ReadWriteMany). The ST4SD Datastore uses this PVC to store its MongoDB database.
   - `spec.setup.pvcRuntimeService`: This PVC (e.g. `runtime-service`) should support mounting under multiple pods in Read/Write, 
     filesystem mode (i.e. ReadWriteMany). The ST4SD Runtime Service uses this PVC to store the metadata of virtual experiments of your ST4SD Registry.
2. (Optional) Configure the Internal Experiments feature of st4sd-runtime-service. This switches on the Build Canvas functionality of st4sd-runtime-service thereby enabling users to create experiments using an interactive Build Canvas. These "internal experiments" are stored on a S3 bucket and users have the option of making changes to them using the same Build Canvas feature. To enable this feature record the credentials and information of a S3 bucket in a Kubernetes secret using the following keys:
    - ENDPOINT (required)
    - BUCKET (required)
    - S3_ACCESS_KEY_ID (optional)
    - S3_SECRET_ACCESS_KEY (optional)
    - S3_REGION (optional)
	
    The st4sd-runtime-service will store the DSL 2.0 workflow definitions in the referenced S3 bucket with the prefix `experiments/`.
3. (Optional) Configure the Graph Library feature of st4sd-runtime-service. This feature enables users to access re-usable Graph recipes that are stored in a Graph Library. They can also manage the contents of the library. To enable this feature record the credentials and information of a S3 bucket (can be the same as the one above) in a Kubernetes secret using the following keys:
    - ENDPOINT (required)
    - BUCKET (required)
    - S3_ACCESS_KEY_ID (optional)
    - S3_SECRET_ACCESS_KEY (optional)
    - S3_REGION (optional)
	
    The st4sd-runtime-service will store the Graph Templates in the referenced S3 bucket with the prefix `library/`.
4. Modify the [basic.yaml](examples/basic.yaml) YAML file to modify the names of the PVCs (unless you used the ones we suggested above) and the desired RouteDomain of your ST4SD instance
    - If you are using an OpenShift cluster on IBM Cloud, `st4sd-olm` can auto-detect your cluster ingress. 
      You can use `${CLUSTER_INGRESS}` to reference it in your `spec.setup.routeDomain` field (see example). 
3. Create the [basic.yaml](examples/basic.yaml) YAML file (e.g. `oc apply -f examples/basic.yaml`).

> **Note**: If you have already installed ST4SD Cloud manually using the [st4sd-deployment instructions](https://github.com/st4sd/st4sd-deployment/blob/main/docs/install-requirements.md#storage-setup) then inspect the `deployment-options.yaml` file you created for ST4SD. You can re-use the PersistentVolumeClaim (PVC) objects and `routePrefix` you selected when you deployed ST4SD manually. The st4sd-olm operator will import your existing deployment and automatically keep it up to date.

## References

If you use ST4SD in your projects, please consider citing the following:

```bibtex
@software{st4sd_2022,
author = {Johnston, Michael A. and Vassiliadis, Vassilis and Pomponio, Alessandro and Pyzer-Knapp, Edward},
license = {Apache-2.0},
month = {12},
title = {{Simulation Toolkit for Scientific Discovery}},
url = {https://github.com/st4sd/st4sd-runtime-core},
year = {2022}
}
```
