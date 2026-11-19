# Kubebuilder operators workshop

Vandaag gaan we aan de slag met k8s-operators. Hiervoor gebruiken we Kubebuilder, een framework om k8s API's te schrijven.
Kubebuilder volgt het [operator-pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/). Een operator wordt gebruikt om de state van een service te bewaken. Dat houdt in
dat deze de resources aanmaakt, kan updaten en kan verwijderen indien nodig. Ook kan een operator reageren als er iets mis is.

In deze workshop gaan we aan de slag met een database upgrade. We migreren Postgres van v16 naar v17 zonder downtime.

## Vereisten

- [Go 1.26+](https://golang.org/dl/)
- [Docker](https://docs.docker.com/get-docker/)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- Een IDE (GoLand, IntelliJ, VSCode)

## Installatie

1. Clone deze repo.
2. Run `./workshop/setup.sh` vanaf de repo root.
3. `setup.sh` maakt een eigen kubeconfig voor je kind-cluster aan. Zet de kubeconfig-env: `export KUBECONFIG=$PWD/workshop/kubeconfig.yaml` (vanuit de root van de repo, en in elke
nieuwe terminal-sessie).
4. Controleer of het kind-cluster is gestart: `kubectl get pods -A` (er moeten pods verschijnen).

## Verbinden met de database
Zodra de operator een database heeft gestart, kun je er via de client service (`postgres-primary`) mee verbinden. Met
onderstaand cmd start je een tijdelijke pod die de versie opvraagt:

```shell
kubectl run psql --rm -it --restart=Never -n postgres-example --image=postgres:17 \
  --env PGPASSWORD=workshop -- psql -h postgres-primary -U postgres -d app -c 'SELECT version();'
```

## Stap 1: De CRD
Een van de handige tools van Kubebuilder is de mogelijkheid om een Custom Resource Definition (CRD, een soort van
"config" voor de resources die je wilt deployen) vanuit de code te genereren, dus let's get started. 

1. Om de CRD te bewerken moeten we in `api/v1/postgres_types.go` zijn. Laten we beginnen met het toevoegen van een versie aan de CRD zodat de operator straks weet welke versie hij moet managen. De `PostgresSpec` is de plek om dit soort config te gaan toevoegen.
De spec komt terug als field van `Postgres`, de uiteindelijke CRD. Voor nu houden we het simpel: We focussen ons op de
major-versie. Voeg een `Version` veld toe met het type `int`.
2. Het veld heeft ook een JSON-tag nodig. In `go` voeg je dit soort tags toe door *na* het veld een tag tussen
backticks te zetten. Hiervoor kun je `` `json:"<veldnaam>"` `` achter het betreffende field zetten.

   <details>
   <summary>Code</summary>

   ```go
   type PostgresSpec struct {
       Version int `json:"version"`
   }
   ```

   </details>

3. Je kunt met Kubebuilder ook annotaties zetten om instructies mee te geven voor het genereren van de CRD. Hiermee kun 
je op een simpele manier je CRD valideren. Kubernetes/je operator zal de CRD's ook niet accepteren als ze niet aan de 
validatie voldoen. Ook kun je bijv. defaults zetten. Beide gaan we gebruiken. Voor nu supporten we alleen versies 16 en 
17 voor `postgres`. De versie is verplicht (`// +required`) en we geven een min en max versie mee 
(`// +kubebuilder:validation:Minimum=16` en `Maximum` voor de maximale versie).

   <details>
   <summary>Code</summary>

   ```go
   type PostgresSpec struct { // d.m.v. Kubebuilder annotaties kun je validatie aan je velden toevoegen
       // De bovenste comment-regels vormen de beschrijving van je veld in de CRD. We accepteren alleen v16 en v17 en het
       // veld is verplicht
       // +kubebuilder:validation:Minimum=16
       // +kubebuilder:validation:Maximum=17
       // +required
       Version int `json:"version"`
   }
   ```
   </details>
4. Almost there. Laten we de daadwerkelijke CRD genereren met `make install`.
5. Tijd om een minimale CR (CustomResource, CRD is de blauwprint, de CR is een instantie daarvan) op te zetten, bijvoorbeeld in `config/samples/postgres_cr.yaml`.
   `kind: Postgres` (deze hebben we net gemaakt) en
   `apiVersion: database.cinqict.nl/v1`. De spec is zoals je 'm geconfigureerd hebt:
   <details>
   <summary>Code</summary>

   ```yaml
      apiVersion: database.cinqict.nl/v1
      kind: Postgres
      metadata:
        name: postgres
        namespace: postgres-example
      spec:
        version: 16 # je kunt nog 15 of 18 proberen om je validatie in werking te zien
   ```
   </details>

## Stap 2: de `Reconcile`-loop
De Reconcile-loop is de functie waarbij de operator het cluster naar de gewenste state brengt. In dit geval gaat hij de 
postgres-versie voor je managen. We gaan een statefulset aanmaken om de postgres pod te laten starten.

Er staat al wat code klaar om dit te realiseren, maar er ontbreken hier en daar nog wat dingen. De kant-en-klare bouwblokken (de headless service, de postgres-container en de PVC) staan in `internal/controller/postgres_blocks.go`. Die hoef je niet aan te passen. Jij zet ze in elkaar in `internal/controller/postgres_controller.go`.

1. Begin met `statefulSetSpec`. Dit is de blauwdruk van je StatefulSet. De `name` en de `ServiceName` staan er al. Vul de rest aan:
   - vars:
     - `labels`: een `map[string]string` met `app: <name>`. De headless service zoekt zijn pods op precies dit label, dus houd het gelijk.
     - `replicas`: eentje lijkt me wel genoeg toch? Let op: de StatefulSet wil een `*int32`, dus je moet een pointer meegeven.
     - `pgContainer`: die haal je op met `postgresContainer(postgres, version)`.
   - fields:
     - `Replicas`: het aantal replicas dat je net hebt gemaakt.
     - `Selector`: een `metav1.LabelSelector` met `MatchLabels`. Dit moet matchen met de labels van de pod, anders weet de StatefulSet niet welke pods van hem zijn.
     - `Template.ObjectMeta`: zet hier dezelfde labels.
     - `Template.Spec`: een `corev1.PodSpec`. In dit geval zijn dat alleen de containers, dus je container uit het derde puntje

   <details>
   <summary>Code</summary>

   ```go
   func statefulSetSpec(postgres *databasev1.Postgres, version int) appsv1.StatefulSetSpec {
       name := fmt.Sprintf("%s-%d", postgres.Name, version)
       labels := map[string]string{"app": name}
       replicas := int32(1)
       pvc := getPVC()

       return appsv1.StatefulSetSpec{
           ServiceName: name,
           Replicas:    &replicas,
           Selector:    &metav1.LabelSelector{MatchLabels: labels},
           Template: corev1.PodTemplateSpec{
               ObjectMeta: metav1.ObjectMeta{Labels: labels},
               Spec: corev1.PodSpec{
                   Containers: []corev1.Container{postgresContainer(postgres, version)},
               },
           },
           VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
               pvc,
           },
           PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
               WhenDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
               WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
           },
       }
   }
   ```

   </details>

2. Nu `ensureDatabase`. Deze functie zorgt dat de StatefulSet daadwerkelijk staat en geeft terug of hij `ready` is. De
headless service wordt al voor je geregeld (deze zorgt voor een vast adres voor je pod). Binnen de `CreateOrUpdate` 
moet je twee dingen doen:
   - Zet `sts.Spec` alleen als de StatefulSet nog niet bestaat (`sts.CreationTimestamp.IsZero()`). De meeste velden van
     een StatefulSet zijn namelijk immutable, dus als je ze elke reconcile opnieuw zet krijg je errors van de API.
   - Maak de CR eigenaar van de StatefulSet met `controllerutil.SetControllerReference`. Dan ruimt k8s alles op als je 
     de CR verwijdert (gc) en hoef je er in principe niks voor te doen.

3. Vergeet daarna niet de error af te handelen. Het laatste wat de functie teruggeeft is of de database klaar is: dat is zo als `sts.Status.ReadyReplicas == 1`. Haal ook de placeholder-regels (`_ = sts`, `_ = controllerutil.CreateOrUpdate`
  en de `fmt.Errorf("... nog niet geïmplementeerd")`) weg. 
  Die staan er alleen zodat go compileert (anders krijg je unused variables en import errors).

   <details>
   <summary>Code</summary>

   ```go
   func (r *PostgresReconciler) ensureDatabase(ctx context.Context, postgres *databasev1.Postgres, version int) (bool, error) {
       name := versionedName(postgres, version)
       if err := r.ensureHeadlessService(ctx, postgres, name); err != nil {
           return false, err
       }

       sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: postgres.Namespace}}

       _, err := controllerutil.CreateOrUpdate(ctx, r.Client, sts, func() error {
           // de meeste velden zijn immutable, dus alleen zetten bij het aanmaken
           if sts.CreationTimestamp.IsZero() {
               sts.Spec = statefulSetSpec(postgres, version)
           }
           return controllerutil.SetControllerReference(postgres, sts, r.Scheme)
       })
       if err != nil {
           return false, err
       }

       return sts.Status.ReadyReplicas == 1, nil
   }
   ```

   </details>

4. Als laatste moeten we `ensureDatabase` ook echt aanroepen vanuit `Reconcile`, anders gebeurt er nog niks. Vervang de `TODO (stap 2)` door een call met `postgres.Spec.Version`. Geeft hij een error terug? Dan geef je die terug aan controller-runtime (gewoon, door een leeg `Result` terug te geven), dan probeert 'ie het later vanzelf opnieuw. 
 Is de database nog niet klaar? Dan geef je ook een Result terug, maar met `RequeueAfter: 5 * time.Second` (en geven we
 de database iets langer de tijd). 

   <details>
   <summary>Code</summary>

   ```go
   ready, err := r.ensureDatabase(ctx, postgres, postgres.Spec.Version)
   if err != nil {
       return ctrl.Result{}, err
   }
   if !ready {
       log.Info("Waiting for Postgres to become ready", "name", postgres.Name)
       return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
   }
   ```

   </details>

5. Tijd om het te testen. Laad je CRD en run de operator lokaal met `make install run`. Apply daarna je CR uit stap 1 (`kubectl apply -f config/samples/postgres_crd.yaml`) en kijk met `kubectl get statefulset,pods -n postgres-example` of je database opkomt.
 Verbind er mee zoals beschreven in [Verbinden met de database](#verbinden-met-de-database) en draai `SELECT version();`. Als het goed is zie je Postgres 16. 
 Verwijder je de CR? Dan zie je dat de StatefulSet en de service vanzelf opgeruimd worden. Dat komt omdat we de owner-references
 hebben gezet, waardoor k8s garbage collection zijn werk kan doen. De PVC's zijn hier een uitzondering op: StatefulSets
 bewaren die standaard ook als de StatefulSet verwijderd wordt. Met `PersistentVolumeClaimRetentionPolicy` ruimen we ze
 in dit voorbeeld ook op bij verwijderen van de StatefulSet (`WhenDeleted: Delete`). 
 Dit geldt dus ook als na een migratie de StatefulSet van de oude versie wordt opgeruimd.

## Stap 3: Migreren zonder downtime
Nu draait de database, maar de operator weet nog niet wat hij moet doen als je de versie in de CR aanpast. Daar gaan we nu voor zorgen. Het lastige is dat de operator moet onthouden welke versie er *nu* draait, terwijl `Spec.Version` al de *nieuwe* versie is.

1. We beginnen met onthouden wat er draait. Zet `Status.CurrentVersion` op `Spec.Version` als die nog niet gezet is (dus `0`) en sla hem op met `r.Status().Update(ctx, postgres)`. Let op: gebruik hier `Status().Update` en niet `Update`, want de status is een subresource.

2. Check daarna of er een migratie nodig is. Dat is zo als `Status.CurrentVersion` gezet is en afwijkt van `Spec.Version`.
   - Zo ja: roep `r.Migrate` aan en geef het resultaat terug. `Migrate` plant zichzelf opnieuw in (`RequeueAfter`) zolang de migratie bezig is, dus je hoeft zelf niet te wachten.
     - Je kunt hier nog een stapje verder in gaan door te checken of de versie hoger is. Dit zou je zelfs in de CRD kunnen valideren, maar voor nu slaan we dat even over.
   - Zo nee: ga gewoon door met `ensureDatabase`.

3. Pas je `ensureDatabase`-aanroep uit stap 2 aan. Tijdens een migratie draait de oude database nog gewoon door, dus je moet hem aanroepen met de versie die *nu* draait (de helper `currentVersion` regelt dit voor je) in plaats van `Spec.Version`.

   <details>
   <summary>Code</summary>

   ```go
   // is er een migratie nodig?
   if postgres.Status.CurrentVersion != 0 && postgres.Status.CurrentVersion != postgres.Spec.Version {
       return r.Migrate(ctx, postgres)
   }

   ready, err := r.ensureDatabase(ctx, postgres, currentVersion(postgres))
   if err != nil {
       return ctrl.Result{}, err
   }
   if !ready {
       return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
   }

   // onthoud wat er draait
   if postgres.Status.CurrentVersion == 0 {
       postgres.Status.CurrentVersion = postgres.Spec.Version
       if err := r.Status().Update(ctx, postgres); err != nil {
           return ctrl.Result{}, err
       }
   }
   ```

   </details>

4. Laten we het in actie zien. Draai de operator weer, zet in je CR `version: v17` en apply hem opnieuw. Volg wat er gebeurt met `kubectl get statefulset,jobs,pods -n postgres-example -w`. Als het goed is komt er een tweede database (v17) naast de oude te staan, wordt de data gemigreerd en blijft de client service al die tijd bereikbaar. Verbind weer (zie [Verbinden met de database](#verbinden-met-de-database)), run `SELECT version();` en je zou nu Postgres v17 moeten zien.
