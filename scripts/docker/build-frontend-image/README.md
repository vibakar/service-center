## Service Center Frontend

This script helps you to make the docker image of service-center frontend present in "PROJECT_ROOT/frontend" folder. Where PROJECT_ROOT is project's top level folder. The script file "PROJECT_ROOT/scripts/frontend/start_linux.sh" will be the entrypoint for docker container.

### How To Run

This script assumes you have already downloaded all the dependencies using 'glide install'. Make sure service-center application is running and get service-center applications IP and PORT addresses.

Update "PROJECT_ROOT/frontend/conf/app.conf" with service-center applications IP and PORT address.

Example app.conf file,

	frontend_host_ip=0.0.0.0
	frontend_host_port=30103

Note: Configuring SC_HOST_IP will enable you to setup the EIP of the Service-center.

Then build docker image of frontend from folder "PROJECT_ROOT/scripts/docker/build-frontend-image/".


    bash -x build.sh
    
Once the script finishes you will see image scfrontend-dev.tgz in the same directory. You can use this this image to load the docker and start using. Run following command, open Web Browser and connect to URL "http://<HOST_IP>:30103" to view frontend UI.

    docker run -d -p 30103:30103 -t servicecomb/scfrontend

Where HOST_IP is the ip of host machine where docker container for frontend is executing.

Note: The same image can be used to deploy in Huawei Public Cloud using CFE/CCE or just deploy in a VM.
