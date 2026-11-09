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

## Stap 0: De CRD
Een van de handige tools van Kubebuilder is de mogelijkheid om een Custom Resource Definition (CRD, een soort van
"config" voor de resources die je wilt deployen) vanuit de code te genereren, dus let's get started. 
1. Gebruik `kubebuilder create api` om een `API` te genereren. De volgende params kunnen worden meegegeven: 
   - `--group`: Dit is de functionele groep waaronder de CRD valt - in dit geval volstaat `database`
   - `--version`: Dit is de versie van de CRD. 't Is de eerste versie, dus: `v1`
   - `--kind`: Dit is de naam van de CRD: We gaan `Postgres` managen, dus je raadt het al: `Postgres`
   - Voor de volledigheid: `kubebuilder create api --group database --version v1 --kind Postgres`
   - We willen zowel een `resource` als een `controller`, antwoord op beide vragen "ja" (je kunt ook 
`--resource --controller` meegeven aan bovenstaand commando)
2. Als het goed is zijn er resources aangemaakt onder `internal` en `api`
3. Om de CRD te bewerken moeten we in `api/v1/postgres_types.go` zijn
4. Met `make generate` laten we `kubebuilder` daadwerkelijke manifests genereren. Let's give it a shot: 
Verander het `Foo`-veld om te valideren. Als het goed is moet je nu in `config/samples/database_v1_postgres.yaml` een 
voorbeeld zien staan van je eerste CRD met kubebuilder! 

5. Genoeg voorbereiding en gespeel. Laten we beginnen met het echte werk: Laten we een versie aan de CRD toevoegen zodat
de operator straks weet welke versie hij moet managen. De `PostgresSpec` is de plek om dit soort config te gaan toevoegen.
De spec komt terug als field van `Postgres`, de uiteindelijke CRD. Voor nu houden we het simpel: We focussen ons op de
major-versie. Voeg een `Version` veld toe met het type `int`.
7. Beide velden hebben ook JSON-tags nodig. In `go` voeg je dit soort tags toe door *na* het veld een tag tussen
   backticks te zetten. Hiervoor kun je `json: <veldnaam>` achter het betreffende field zetten.

   <details>
   <summary>Code</summary>

   ```go
   type PostgresSpec struct {
       Version int `json:"version"`
   }
   ```

   </details>

8. Je kunt met kubebuilder ook annotaties zetten om instructies mee te geven voor het genereren van de CRD. Hiermee kun 
je op een simpele manier je CRD valideren. Kubernetes/je operator zal de CRD's ook niet accepteren als ze niet aan de 
validatie voldoen. Ook kun je bijv. defaults zetten. Beide gaan we gebruiken. Voor nu supporten we alleen versies 16 en 
17 voor `postgres`. De versie is verplicht (`// +required`) en we geven een min en max versie mee 
(`// +kubebuilder:validation:Minimum=16` en `Maximum` voor de maximale versie)
   <details>
   <summary>Code</summary>
   type PostgresSpec struct { // d.m.v. kubebuilder annotaties kun je validatie aan je velden toevoegen
   // De bovenste comment-regels vormen de beschrijving van je veld in de CRD. We accepteren alleen v16 en 17 en het 
   //veld is verplicht
   //  +kubebuilder:validation:Minimum=16
   // +kubebuilder:validation:Maximum=17
   // +required
   Version int `json:"version"`
   }
   </details>