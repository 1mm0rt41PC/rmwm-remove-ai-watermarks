# Validation de rmwm 0.3.0

Date : 20 octobre 2026. Environnement de construction : Linux x86-64,
Go 1.27.3, module ciblant Go 1.25 ou supérieur. Aucune dépendance Go externe.

## Résultats obtenus

| Vérification | Résultat |
| --- | --- |
| `go test -race -count=1 ./...` sous Linux | Réussi |
| Fuzz d'idempotence avec les tirets et les 32 combinaisons d'options | Réussi : 62 936 exécutions en 3 secondes, 384 cas de départ |
| `go vet ./...` sous Linux | Réussi |
| `GOOS=windows GOARCH=amd64 go vet ./...` | Réussi |
| Compilation de la suite de tests pour Windows x64 | Réussie ; tests non exécutés sous Windows |
| Construction `CGO_ENABLED=0` du binaire Windows x64 | Réussie ; PE console, sans option de liaison GUI globale |
| Construction `CGO_ENABLED=0` du binaire Linux x64 | Réussie ; ELF statique |
| Vérification `gofmt -l .` | Aucun fichier à reformater |
| Essais du binaire Linux livré | Réussis : version 0.3.0, stdin exact, UTF-16 LE avec BOM, tirets JSON échappés, collisions de clés, codes `-check`, désactivation, compteurs et aide |

Les ajouts de la version 0.3.0 couvrent :

- Remplacement de `—` (`U+2014`), `⸺` (`U+2E3A`) et `⸻` (`U+2E3B`), chacun
  par un seul tiret ASCII `-`, avec un remplacement comptabilisé par caractère.
- Activation par défaut dans le véritable parseur CLI ; conservation avec
  `-no-normalize-dashes`, indépendante de `-no-normalize-spaces`.
- Préservation des autres tirets voisins et du signe moins, des espaces,
  des tabulations, des fins de ligne et des accents.
- Stdin, fichier avec contrôle sans écriture puis sauvegarde `.bak`,
  presse-papiers injecté, texte JSON réel/échappé et refus des collisions de clés.
- Transmission des quatre combinaisons indépendantes de normalisation des
  espaces et des tirets au parseur du worker Windows.

La suite couvre également :

- `~` : nettoyage sur place, conservation des CRLF/LF et absence de contenu
  sur stdout ; compteurs sur stderr uniquement.
- Absence d'écriture si texte inchangé, `-check`, `-dry-run`, texte non valide,
  dépassement de taille, JSON incorrect, collision de clés ou copie remplacée
  entre l'analyse et l'écriture.
- Transmission de toutes les options de nettoyage, du format et de la limite
  de taille aux arguments du worker, puis au véritable parseur CLI injecté.
- Décodage UTF-16 Windows exécuté sous Linux grâce à une fonction pure :
  limites exactes, surrogates, terminaison NUL, caractères non BMP et fins de ligne.
- Linux : sélection Wayland/X11, `CLIPBOARD` explicite, MIME et cibles admissibles,
  images/fichiers ignorés, absence de bascule silencieuse entre backends.
- Linux : exécution de subprocess de test pour les octets exacts, les limites
  mémoire, les diagnostics bornés, les délais et les processus propriétaires
  conservant leurs descripteurs après sortie du processus initial.

Les tests de commandes Linux utilisent des backends injectés et le binaire
de test, jamais un presse-papiers utilisateur. L'environnement des seuls helpers
de test désactive le délai de sortie artificiel du détecteur de courses Go ;
les délais de production ne sont pas modifiés.

## Revue Windows et limite de cette validation

Les appels Win32 et les structures `WNDCLASSW`/`MSG` ont été relus :
propriétaire HWND non nul, ouverture/fermeture sur un thread OS fixé,
allocation native avant remplacement, comparaison séquence + contenu sous
verrou, nettoyage des ressources et tentative de restauration en cas d'erreur.
La boucle de notifications reste distincte du thread de dialogue.

Le worker utilise `CREATE_NO_WINDOW`. Le masquage de la console du lanceur
vérifie qu'il est l'unique PID attaché. Le démarrage attend un événement de
confirmation propre au lancement ; l'arrêt attend la fin du processus.

**Aucune exécution interactive sur Windows, GNOME ou XFCE n'a été possible
dans cet environnement.** Les tests et la compilation ne constituent donc pas
une validation du rendu de `MessageBoxW`, du masquage effectif sur chaque hôte
de console, ni de l'interopérabilité avec chaque gestionnaire de presse-papiers.

Une console d'un exécutable lancé directement peut apparaître brièvement avant
que son code puisse la masquer. Sous Linux, comparaison et écriture par les
utilitaires externes ne forment pas une transaction atomique. Sous Windows,
`-bg-stop` peut signaler un délai dépassé après quatre secondes si une API native
est bloquée ; le worker continue alors son arrêt gracieux, sans interrompre un
remplacement ou une restauration engagée.

## Essais interactifs conseillés

Ces essais remplacent volontairement le contenu courant du presse-papiers.

### Windows

Dans PowerShell, depuis le dossier de l'exécutable :

```powershell
# Préparer un texte contenant U+200B
Set-Clipboard -Value ("Bon" + [char]0x200B + "jour")

# Vérifier : le code attendu est 1 et le contenu n'est pas modifié
.\rmwm.exe '~' -check
$LASTEXITCODE

# Nettoyer : Get-Clipboard doit ensuite afficher Bonjour
.\rmwm.exe '~'
Get-Clipboard

# Démarrer la surveillance, puis recopier le texte d'essai
.\rmwm.exe -bg
Set-Clipboard -Value ("Bon" + [char]0x200B + "jour")

# Après essai de la boîte de dialogue, arrêter
.\rmwm.exe -bg-stop
```

Vérifier Oui puis Non sur des copies distinctes, une nouvelle copie pendant
le dialogue, l'arrêt pendant le dialogue et le lancement depuis un raccourci
dont la cible se termine par `-bg`. Le terminal appelant doit rester visible
et disponible ; le worker doit fonctionner sans console visible.

Pour la normalisation des tirets, recopier `A—B⸺C⸻D` : le contrôle doit
signaler trois remplacements et le nettoyage produire `A-B-C-D`.
Avec `-no-normalize-dashes`, ce texte doit rester intact. Arrêter le moniteur
avant de remplacer l'exécutable, puis le relancer avec les options choisies.

### Linux

Depuis un terminal de la session graphique, avec le backend installé :

```sh
# Wayland
printf 'Bon\342\200\213jour\n' | wl-copy
./linux-amd64/rmwm '~' -check
./linux-amd64/rmwm '~'
wl-paste --no-newline

# X11
printf 'Bon\342\200\213jour\n' | xclip -selection clipboard
./linux-amd64/rmwm '~' -check
./linux-amd64/rmwm '~'
xclip -selection clipboard -out
```

Les sorties attendues sont `Bonjour` avec la fin de ligne préparée. Tester
également une image et une copie de fichiers : elles doivent rester intactes.
