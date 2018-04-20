// This is literally `helm init -o json` from helm version v2.7.2
{
    "apiVersion": "extensions/v1beta1",
    "kind": "Deployment",
    "metadata": {
        "creationTimestamp": null,
        "labels": {
            "app": "helm",
            "name": "tiller"
        },
        "name": "tiller-deploy",
        "namespace": "kube-system"
    },
    "spec": {
        "strategy": {},
        "template": {
            "metadata": {
                "creationTimestamp": null,
                "labels": {
                    "app": "helm",
                    "name": "tiller"
                }
            },
            "spec": {
                "containers": [
                    {
                        "env": [
                            {
                                "name": "TILLER_NAMESPACE",
                                "value": "kube-system"
                            },
                            {
                                "name": "TILLER_HISTORY_MAX",
                                "value": "0"
                            }
                        ],
                        "image": "gcr.io/kubernetes-helm/tiller:v2.8.2",
                        "imagePullPolicy": "IfNotPresent",
                        "livenessProbe": {
                            "httpGet": {
                                "path": "/liveness",
                                "port": 44135
                            },
                            "initialDelaySeconds": 1,
                            "timeoutSeconds": 1
                        },
                        "name": "tiller",
                        "ports": [
                            {
                                "containerPort": 44134,
                                "name": "tiller"
                            }
                        ],
                        "readinessProbe": {
                            "httpGet": {
                                "path": "/readiness",
                                "port": 44135
                            },
                            "initialDelaySeconds": 1,
                            "timeoutSeconds": 1
                        },
                        "resources": {}
                    }
                ]
            }
        }
    },
    "status": {}
}
