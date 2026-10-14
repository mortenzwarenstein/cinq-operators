# Kubebuilder operators workshop

Vandaag gaan we aan de slag met k8s-operators. Hiervoor gebruiken we kubebuilder, een framework om k8s API's te schrijven.
Kubebuilder volgt het [operator-pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/). Een operator wordt gebruikt om de state van een service te bewaken. Dat houdt in
dat deze de resources aanmaakt, kan updaten en kan verwijderen indien nodig. Ook kan een operator reageren als er iets mis is.

In deze workshop gaan we aan de slag met een database upgrade. We migreren Postgres van v16 naar v17 zonder downtime.

## Vereisten

- [Go 1.26+](https://golang.org/dl/)
- [Docker](https://docs.docker.com/get-docker/)
- [Kubernetes](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- An IDEA (IntelliJ IDEA, VSCode)

## Installatie

1. Clone deze repo.
2. Run `setup.sh` in de `./workshop` directory.
3. Controleer de volgende zaken:
   - Check: `kubectl get pods -n postgres-example` of de database is gestart. 
   - Check of je kunt verbinden (gebruik je favo tool & `kubectl port-forward <pod_name> 5432:5432`). 
   - Run `SELECT version();`. Output zou aan moeten geven dat het om Postgres 16 gaat.
