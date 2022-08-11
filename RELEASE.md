## Releasing new `k8s-shredder` version

For release process we're using [`goreleaser`](https://goreleaser.com/). You must install it first before being able to
release a new version.
Config file for `goreleaser` can be found in [goreleaser file](.goreleaser.yml)

GoReleaser requires an API token with the `repo` scope selected to deploy the artifacts to GitHub.
For generating a new token, you can create one from [tokens section](https://github.com/settings/tokens/new). For more details see 
[creating-a-personal-access-token](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/creating-a-personal-access-token)

For publishing a new release follow below steps:

```
export NEW_VERSION=vX.Y.Z
git tag -a ${NEW_VERSION} -m "Release ${NEW_VERSION}"
git push origin ${NEW_VERSION}

export GITHUB_TOKEN=<your_github_PAT_token> 
make publish
```

You can check if the new release and associated artifacts were properly pushed into GitHub by accessing
[k8s-shredder releases](https://github.com/adobe/cluster-registry/releases)