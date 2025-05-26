
#!/bin/bash

pods=$(kubectl get fastpods -n fast-gshare-fn --no-headers | awk '{print $1}')

# Delete each pod
for pod in $pods; do
  kubectl delete fastpod "$pod" -n fast-gshare-fn
done