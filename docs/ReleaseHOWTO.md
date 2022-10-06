# How to generate a new release to harvester

1. Tag the version on [harvester/node-disk-manager](https://github.com/harvester/node-disk-manager)
 repository.

    (Use the github release action. Draft a new release with the related version and its commit.)

2. Bump the new version on [harvester/charts](https://github.com/harvester/charts)

    - If any chart changes, it should work on the `master` branch first then backport it to the `release` branch.
    - If no chart changes, work on the `release` branch
    - Now, the `version` and `apiVersion` are synced on NDM. That means no matter if you want to change the content of chart or update image you need to bump these two versions.

    (Check the [here](https://github.com/harvester/charts/tree/master/charts/harvester-node-disk-manager) would help to know more detail.)

3. Bump the version on [harvester/harvester](https://github.com/harvester/charts)

    - Working on [deploy/charts/harvester](https://github.com/harvester/harvester/tree/master/deploy/charts/harvester)
    - The node-disk-manager is a Harvester dependency chart. You need to bump the related version (as above) and update the chart. (Command: `helm dependency`)
    - Remember that the values.yaml is only bumped on the release branch. On the master branch, we keep it to master-head.